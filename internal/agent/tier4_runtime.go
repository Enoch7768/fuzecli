package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/security"
)

type tier4State struct {
	Memories []map[string]any `json:"memories"`
	Profile map[string]any `json:"profile"`
	Feedback []map[string]any `json:"feedback"`
}

func (rt ToolRuntime) tier4StatePath() string {
	return filepath.Join(rt.Root, ".aicli", "agent_state.json")
}

func (rt ToolRuntime) loadTier4State() (tier4State, error) {
	s := tier4State{Memories: []map[string]any{}, Profile: map[string]any{}, Feedback: []map[string]any{}}
	b, err := os.ReadFile(rt.tier4StatePath())
	if errors.Is(err, os.ErrNotExist) { return s, nil }
	if err != nil { return s, err }
	if err := json.Unmarshal(b, &s); err != nil { return s, fmt.Errorf("read FuzeCLI agent state: %w", err) }
	if s.Memories == nil { s.Memories = []map[string]any{} }
	if s.Profile == nil { s.Profile = map[string]any{} }
	if s.Feedback == nil { s.Feedback = []map[string]any{} }
	return s, nil
}

func (rt ToolRuntime) saveTier4State(s tier4State) error {
	dir := filepath.Dir(rt.tier4StatePath())
	if err := os.MkdirAll(dir, 0700); err != nil { return err }
	b, err := json.MarshalIndent(s, "", "  "); if err != nil { return err }
	tmp := rt.tier4StatePath()+".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil { return err }
	if err := os.Rename(tmp, rt.tier4StatePath()); err != nil { _ = os.Remove(tmp); return err }
	return nil
}

func tier4HTTP(ctx context.Context, rawURL string, headers map[string]string) ([]byte, http.Header, int, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" { return nil,nil,0,errors.New("URL must be a valid HTTP(S) URL") }
	if strings.Contains(u.Hostname(), "%") || strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") { return nil,nil,0,errors.New("local hosts are not allowed by this web tool") }
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) { return nil,nil,0,errors.New("private or loopback IP addresses are not allowed") }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil); if err != nil { return nil,nil,0,err }
	req.Header.Set("User-Agent", "FuzeCLI/1.0 (+https://github.com/Enoch7768/fuzecli)")
	req.Header.Set("Accept", "text/html,application/json,application/rss+xml,application/xml;q=0.9,*/*;q=0.8")
	for k,v := range headers { req.Header.Set(k,v) }
	client := &http.Client{Timeout: 15*time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { if len(via)>=4 { return errors.New("too many redirects") }; host:=strings.ToLower(req.URL.Hostname()); if host=="localhost" || strings.HasSuffix(host,".localhost") { return errors.New("redirect to local host blocked") }; if ip:=net.ParseIP(host); ip!=nil&&(ip.IsPrivate()||ip.IsLoopback()||ip.IsLinkLocalUnicast()||ip.IsUnspecified()){return errors.New("redirect to private IP blocked")}; return nil }}
	resp, err := client.Do(req); if err != nil { return nil,nil,0,err }; defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)); if err != nil { return nil,nil,resp.StatusCode,err }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return b,resp.Header,resp.StatusCode,fmt.Errorf("HTTP %d from %s",resp.StatusCode,u.Host) }
	return b,resp.Header,resp.StatusCode,nil
}

func tier4HTMLText(s string) string {
	re := regexp.MustCompile(`(?is)<(script|style|noscript|svg)[^>]*>.*?</\1>`)
	s = re.ReplaceAllString(s, " ")
	re = regexp.MustCompile(`(?s)<[^>]+>`)
	s = re.ReplaceAllString(s, " ")
	for _, pair := range [][2]string{{"&amp;","&"},{"&quot;",`"`},{"&#39;","'"},{"&lt;","<"},{"&gt;",">"},{"&nbsp;"," "}} { s=strings.ReplaceAll(s,pair[0],pair[1]) }
	return strings.Join(strings.Fields(s), " ")
}
func tier4HTMLAttr(tag, attr string) string {
	re := regexp.MustCompile(`(?is)\b`+regexp.QuoteMeta(attr)+`\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	m:=re.FindStringSubmatch(tag); if len(m)==0{return ""}; for i:=1;i<len(m);i++{if m[i]!=""{return m[i]}};return ""
}
type tier4SearchItem struct { Title string `json:"title"`; URL string `json:"url"`; Snippet string `json:"snippet"`; Rank int `json:"rank"`; Engine string `json:"engine,omitempty"` }
func (rt ToolRuntime) tier4SearchOne(ctx context.Context, engine string, a map[string]any) ([]tier4SearchItem,error) {
	q:=stringArg(a,"query"); n:=intArg(a,"max_results",10); if n<1{n=10}; if n>20{n=20}
	var endpoint string
	switch engine {
	case "google":
		u,_:=url.Parse("https://www.google.com/search"); v:=u.Query();v.Set("q",q);v.Set("num",fmt.Sprint(min(n,10)));if lang:=stringArg(a,"language");lang!=""{v.Set("hl",lang)};if region:=stringArg(a,"region");region!=""{v.Set("gl",region)};if tr:=stringArg(a,"time_range");tr!=""&&tr!="any"{m:=map[string]string{"day":"d","week":"w","month":"m","year":"y"};v.Set("tbs","qdr:"+m[tr])};u.RawQuery=v.Encode();endpoint=u.String()
	case "bing":
		u,_:=url.Parse("https://www.bing.com/search");v:=u.Query();v.Set("q",q);v.Set("count",fmt.Sprint(n));if region:=stringArg(a,"region");region!=""{v.Set("cc",region)};if lang:=stringArg(a,"language");lang!=""{v.Set("setlang",lang)};u.RawQuery=v.Encode();endpoint=u.String()
	default:
		u,_:=url.Parse("https://html.duckduckgo.com/html/");v:=u.Query();v.Set("q",q);u.RawQuery=v.Encode();endpoint=u.String();engine="duckduckgo"
	}
	body,_,status,err:=tier4HTTP(ctx,endpoint,nil)
	if err!=nil {
		if engine=="google" && (status==429||strings.Contains(strings.ToLower(string(body)),"unusual traffic")||strings.Contains(strings.ToLower(string(body)),"g-recaptcha")) { return nil,fmt.Errorf("Google CAPTCHA or rate limit; try web.search_multi") }
		return nil,err
	}
	html:=string(body); low:=strings.ToLower(html)
	if engine=="google"&&(strings.Contains(low,"unusual traffic")||strings.Contains(low,"g-recaptcha")){return nil,errors.New("Google CAPTCHA or rate limit; try web.search_multi")}
	out:=[]tier4SearchItem{}
	var blocks []string
	switch engine {
	case "google":
		re:=regexp.MustCompile(`(?is)<div class="g"[^>]*>.*?</div>\s*</div>`);blocks=re.FindAllString(html,-1)
		for _,b:=range blocks{title:=tier4HTMLText(regexp.MustCompile(`(?is)<h3[^>]*>.*?</h3>`).FindString(b));m:=regexp.MustCompile(`(?is)<a[^>]+href="([^"]+)"[^>]*`).FindStringSubmatch(b);if title==""||len(m)<2{continue};link:=m[1];if strings.HasPrefix(link,"/url?"){u,_:=url.Parse("https://www.google.com"+link);link=u.Query().Get("q")};sn:=tier4HTMLText(regexp.MustCompile(`(?is)<div class="VwiC3b"[^>]*>.*?</div>`).FindString(b));if validPublicWebURL(link){out=append(out,tier4SearchItem{title,link,sn,len(out)+1,engine})};if len(out)>=n{break}}
	case "bing":
		re:=regexp.MustCompile(`(?is)<li class="b_algo"[^>]*>.*?</li>`);blocks=re.FindAllString(html,-1)
		for _,b:=range blocks{m:=regexp.MustCompile(`(?is)<h2[^>]*>\s*<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`).FindStringSubmatch(b);if len(m)<3{continue};sn:=tier4HTMLText(regexp.MustCompile(`(?is)<p[^>]*>.*?</p>`).FindString(b));if validPublicWebURL(m[1]){out=append(out,tier4SearchItem{tier4HTMLText(m[2]),m[1],sn,len(out)+1,engine})};if len(out)>=n{break}}
	default:
		re:=regexp.MustCompile(`(?is)<div class="result[^"]*"[^>]*>.*?</div>\s*</div>`);blocks=re.FindAllString(html,-1)
		for _,b:=range blocks{m:=regexp.MustCompile(`(?is)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`).FindStringSubmatch(b);if len(m)<3{continue};sn:=tier4HTMLText(regexp.MustCompile(`(?is)result__snippet[^>]*>.*?</`).FindString(b));if validPublicWebURL(m[1]){out=append(out,tier4SearchItem{tier4HTMLText(m[2]),m[1],sn,len(out)+1,engine})};if len(out)>=n{break}}
	}
	if len(out)==0{return out,errors.New("search engine returned no parseable results; it may have changed markup or blocked this request")}
	return out,nil
}
func validPublicWebURL(s string) bool { u,e:=url.Parse(s);if e!=nil||(u.Scheme!="https"&&u.Scheme!="http")||u.Hostname()==""{return false};h:=strings.ToLower(u.Hostname());return h!="localhost"&&!strings.HasSuffix(h,".localhost")&&(net.ParseIP(h)==nil||(!net.ParseIP(h).IsPrivate()&&!net.ParseIP(h).IsLoopback())) }

func (rt ToolRuntime) executeTier4(ctx context.Context,name string,raw json.RawMessage,a map[string]any)(any,error){
	switch name {\n\tcase "web.search_google", "web.search_bing", "web.search_multi", "web.search_news", "web.search_searxng", "web.wikipedia", "web.stackexchange", "web.scholar", "research.learn_topic", "weather.current", "weather.forecast":\n\t\tif err := rt.Capabilities.Require(security.NetworkAccess); err != nil { return nil, err }\n\t}\n\tswitch name {
	case "web.search_google": a["max_results"]=intArg(a,"max_results",5);items,err:=rt.tier4SearchOne(ctx,"google",a);if err!=nil{return map[string]any{"query":a["query"],"results":[]tier4SearchItem{},"count":0,"available":false,"source":"google_html","message":err.Error()},nil};return map[string]any{"query":a["query"],"results":items,"count":len(items),"available":len(items)>0,"source":"google_html"},nil
	case "web.search_bing": items,err:=rt.tier4SearchOne(ctx,"bing",a);if err!=nil{return map[string]any{"query":a["query"],"results":[]tier4SearchItem{},"count":0,"available":false,"source":"bing_html","message":err.Error()},nil};return map[string]any{"query":a["query"],"results":items,"count":len(items),"available":len(items)>0,"source":"bing_html"},nil
	case "web.search_multi": return rt.tier4SearchMulti(ctx,a)
	case "web.search_searxng": return rt.tier4Searx(ctx,a)
	case "web.search_news": return rt.tier4News(ctx,a)
	case "web.wikipedia": return rt.tier4Wikipedia(ctx,a)
	case "web.stackexchange": return rt.tier4StackExchange(ctx,a)
	case "web.scholar": return rt.tier4Scholar(ctx,a)
	case "research.learn_topic": return rt.tier4Research(ctx,a)
	case "weather.current","weather.forecast": return rt.tier4Weather(ctx,name,a)
	case "language.detect": return tier4LanguageDetect(a)
	case "memory.remember": return rt.tier4Remember(a)
	case "memory.recall": return rt.tier4Recall(a)
	case "memory.list": return rt.tier4ListMemories(a)
	case "memory.update": return rt.tier4UpdateMemory(a)
	case "memory.forget": return rt.tier4Forget(a)
	case "profile.get": return rt.tier4ProfileGet(a)
	case "profile.update": return rt.tier4ProfileUpdate(a)
	case "profile.reset": return rt.tier4ProfileReset(a)
	case "interaction.search": return map[string]any{"available":false,"results":[]any{},"message":"No persisted interaction-history index is configured in this workspace; no conversation history was searched."},nil
	case "feedback.record": return rt.tier4Feedback(a)
	default: return nil,fmt.Errorf("unsupported research/personalization tool %q",name)
	}
}

func (rt ToolRuntime) tier4SearchMulti(ctx context.Context,a map[string]any)(any,error){
	engines:=stringArray(a["engines"]);if len(engines)==0{engines=[]string{"duckduckgo","google","bing"}}
	type answer struct{ engine string; items []tier4SearchItem; err error }
	ch:=make(chan answer,len(engines));for _,e:=range engines{go func(engine string){items,err:=rt.tier4SearchOne(ctx,engine,a);ch<-answer{engine,items,err}}(e)}
	all:=[]tier4SearchItem{};used:=[]string{};unavailable:=[]string{}
	for range engines{r:=<-ch;if r.err!=nil{unavailable=append(unavailable,r.engine);continue};used=append(used,r.engine);all=append(all,r.items...)}
	if boolArg(a,"dedupe",true){seen:=map[string]bool{};out:=all[:0];for _,it:=range all{key:=strings.TrimRight(strings.Split(it.URL,"#")[0],"/");if !seen[key]{seen[key]=true;out=append(out,it)}};all=out}
	sort.SliceStable(all,func(i,j int)bool{return all[i].Rank<all[j].Rank});n:=intArg(a,"max_results",10);if n<1{n=10};if len(all)>n{all=all[:n]};for i:=range all{all[i].Rank=i+1}
	return map[string]any{"query":a["query"],"results":all,"count":len(all),"engines_used":used,"engines_unavailable":unavailable,"available":len(all)>0},nil
}
func (rt ToolRuntime) tier4Searx(ctx context.Context,a map[string]any)(any,error){
	base:=strings.TrimRight(strings.TrimSpace(os.Getenv("FUZECLI_SEARXNG_URL")),"/")
	if base==""{return map[string]any{"available":false,"results":[]any{},"message":"FUZECLI_SEARXNG_URL is not configured; use web.search_multi."},nil}
	u,err:=url.Parse(base);if err!=nil||u.Scheme!="https"&&u.Scheme!="http"||u.Hostname()==""{return nil,errors.New("FUZECLI_SEARXNG_URL must be an HTTP(S) URL")}
	q:=u.Query();q.Set("q",stringArg(a,"query"));q.Set("format","json");if lang:=stringArg(a,"language");lang!=""{q.Set("language",lang)};u.RawQuery=q.Encode()
	b,_,_,err:=tier4HTTP(ctx,u.String(),nil);if err!=nil{return map[string]any{"available":false,"results":[]any{},"message":err.Error()},nil}
	var data struct{Results []struct{Title string `json:"title"`;URL string `json:"url"`;Content string `json:"content"`} `json:"results"`}
	if err:=json.Unmarshal(b,&data);err!=nil{return nil,err};n:=intArg(a,"max_results",10);out:=[]tier4SearchItem{};for i,x:=range data.Results{if validPublicWebURL(x.URL){out=append(out,tier4SearchItem{x.Title,x.URL,x.Content,i+1,"searxng"})};if len(out)>=n{break}}
	return map[string]any{"query":a["query"],"results":out,"count":len(out),"available":len(out)>0,"source":"searxng"},nil
}
func (rt ToolRuntime) tier4News(ctx context.Context,a map[string]any)(any,error){
	u,_:=url.Parse("https://news.google.com/rss/search");q:=u.Query();query:=stringArg(a,"query");switch stringArg(a,"freshness"){case "hour":query+=" when:1h";case "week":query+=" when:7d";default:query+=" when:1d"};q.Set("q",query);lang:=stringArg(a,"language");if lang==""{lang="en"};region:=stringArg(a,"region");if region==""{region="US"};q.Set("hl",lang);q.Set("gl",region);q.Set("ceid",region+":"+lang);u.RawQuery=q.Encode()
	b,_,_,err:=tier4HTTP(ctx,u.String(),nil);if err!=nil{return nil,err}
	var feed struct{Items []struct{Title string `xml:"title"`;Link string `xml:"link"`;PubDate string `xml:"pubDate"`;Source string `xml:"source"`} `xml:"channel>item"`}
	if err:=xml.Unmarshal(b,&feed);err!=nil{return nil,err};type article struct{Title string `json:"title"`;Source string `json:"source"`;PublishedAt string `json:"published_at"`;URL string `json:"url"`}
	n:=intArg(a,"max_results",10);out:=[]article{};for _,it:=range feed.Items{date:=it.PubDate;if t,e:=time.Parse(time.RFC1123Z,date);e==nil{date=t.UTC().Format(time.RFC3339)};out=append(out,article{it.Title,it.Source,date,it.Link});if len(out)>=n{break}}
	return map[string]any{"query":a["query"],"articles":out,"count":len(out),"source":"google_news_rss"},nil
}
func (rt ToolRuntime) tier4Wikipedia(ctx context.Context,a map[string]any)(any,error){
	lang:=stringArg(a,"language");if lang==""{lang="en"};for _,r:=range lang{if !(r>='a'&&r<='z'||r>='A'&&r<='Z'||r=='-'){return nil,errors.New("invalid language code")}}
	u:="https://"+strings.ToLower(lang)+".wikipedia.org/api/rest_v1/page/summary/"+url.PathEscape(stringArg(a,"title"))
	b,_,_,err:=tier4HTTP(ctx,u,nil);if err!=nil{return nil,err}
	var v struct{Title string `json:"title"`;Type string `json:"type"`;Extract string `json:"extract"`;ContentURLs struct{Desktop struct{Page string `json:"page"`} `json:"desktop"`} `json:"content_urls"`;Description string `json:"description"`}
	if err:=json.Unmarshal(b,&v);err!=nil{return nil,err};n:=intArg(a,"sentences",5);sent:=regexp.MustCompile(`[.!?]+\s+`).Split(v.Extract,-1);if len(sent)>n{v.Extract=strings.Join(sent[:n]," ")}
	return map[string]any{"title":v.Title,"language":lang,"summary":v.Extract,"description":v.Description,"url":v.ContentURLs.Desktop.Page,"disambiguation":[]string{},"type":v.Type},nil
}
func (rt ToolRuntime) tier4StackExchange(ctx context.Context,a map[string]any)(any,error){
	u,_:=url.Parse("https://api.stackexchange.com/2.3/search/advanced");q:=u.Query();q.Set("order","desc");q.Set("sort","relevance");q.Set("q",stringArg(a,"query"));site:=stringArg(a,"site");if site==""{site="stackoverflow"};q.Set("site",site);q.Set("pagesize",fmt.Sprint(intArg(a,"max_results",10)));q.Set("filter","withbody");if boolArg(a,"accepted_only",false){q.Set("accepted","True")};if v,ok:=a["min_score"];ok{q.Set("min",fmt.Sprint(v))};if tags:=stringArray(a["tags"]);len(tags)>0{q.Set("tagged",strings.Join(tags,";"))};u.RawQuery=q.Encode()
	b,_,_,err:=tier4HTTP(ctx,u.String(),nil);if err!=nil{return nil,err}
	var data struct{Items []struct{Title string `json:"title"`;Link string `json:"link"`;Score int `json:"score"`;Accepted int64 `json:"accepted_answer_id"`;Answers int `json:"answer_count"`} `json:"items"`;Quota int `json:"quota_remaining"`}
	if err:=json.Unmarshal(b,&data);err!=nil{return nil,err};out:=[]any{};for _,it:=range data.Items{out=append(out,map[string]any{"title":it.Title,"url":it.Link,"score":it.Score,"accepted_answer_id":it.Accepted,"answer_count":it.Answers,"answers":[]any{}})}
	return map[string]any{"query":a["query"],"results":out,"count":len(out),"source":"stackexchange","site":site,"quota_remaining":data.Quota},nil
}
func (rt ToolRuntime) tier4Scholar(ctx context.Context,a map[string]any)(any,error){
	u,_:=url.Parse("https://api.semanticscholar.org/graph/v1/paper/search");q:=u.Query();q.Set("query",stringArg(a,"query"));q.Set("fields","title,authors,year,abstract,citationCount,url,openAccessPdf");q.Set("limit",fmt.Sprint(intArg(a,"max_results",10)));if y,ok:=a["year_from"];ok{q.Set("year",fmt.Sprint(y)+"-")};if y,ok:=a["year_to"];ok{if lo,ok2:=a["year_from"];ok2{q.Set("year",fmt.Sprint(lo)+"-"+fmt.Sprint(y))}else{q.Set("year","-"+fmt.Sprint(y))}};u.RawQuery=q.Encode()
	b,_,status,err:=tier4HTTP(ctx,u.String(),nil);if err!=nil{if status==429{return map[string]any{"available":false,"papers":[]any{},"count":0,"source":"semantic_scholar","message":"rate_limit"},nil};return nil,err}
	var data struct{Data []map[string]any `json:"data"`};if err:=json.Unmarshal(b,&data);err!=nil{return nil,err};out:=[]map[string]any{};for _,p:=range data.Data{if boolArg(a,"open_access_only",false)&&p["openAccessPdf"]==nil{continue};if minC,ok:=a["min_citations"].(float64);ok&&numValue(p["citationCount"])<minC{continue};out=append(out,p)}
	return map[string]any{"query":a["query"],"papers":out,"count":len(out),"available":true,"source":"semantic_scholar"},nil
}
func numValue(v any)float64{switch n:=v.(type){case float64:return n;case int:return float64(n)};return 0}
func (rt ToolRuntime) tier4Research(ctx context.Context,a map[string]any)(any,error){
	topic:=stringArg(a,"topic");depth:=stringArg(a,"depth");maxSrc:=intArg(a,"max_sources",5);if depth=="quick"{maxSrc=2};if depth=="deep"{maxSrc=8};if maxSrc>10{maxSrc=10}
	res,err:=rt.tier4SearchMulti(ctx,map[string]any{"query":topic,"max_results":maxSrc*2,"engines":[]any{"duckduckgo","google","bing"},"dedupe":true});if err!=nil{return nil,err}
	obj,_:=res.(map[string]any);items,_:=obj["results"].([]tier4SearchItem);sources:=[]map[string]any{};var combined strings.Builder
	for i,it:=range items{if i>=maxSrc{break};opened,e:=rt.webOpen(ctx,map[string]any{"url":it.URL,"max_chars":8000});if e!=nil{continue};m,_:=opened.(map[string]any);txt,_:=m["text"].(string);sources=append(sources,map[string]any{"title":it.Title,"url":it.URL,"chars":len(txt)});combined.WriteString("\n\n=== "+it.Title+" ("+it.URL+") ===\n\n"+txt)}
	return map[string]any{"topic":topic,"sources":sources,"combined_text":truncate(combined.String(),60000),"count":len(sources),"depth":depth,"note":"Synthesize these sources in your next response. Call memory.remember separately only if the user asked to save findings."},nil
}
func (rt ToolRuntime) tier4Weather(ctx context.Context,name string,a map[string]any)(any,error){
	loc:=strings.TrimSpace(stringArg(a,"location"));if loc==""{return nil,errors.New("location is required")};var lat,lon float64;place:=loc;country:=""
	if parts:=strings.Split(loc,",");len(parts)==2{if _,e:=fmt.Sscanf(strings.TrimSpace(parts[0]),"%f",&lat);e==nil{if _,e=fmt.Sscanf(strings.TrimSpace(parts[1]),"%f",&lon);e==nil{place=loc}else{lat=0;lon=0}}}
	if lat==0&&lon==0{u,_:=url.Parse("https://geocoding-api.open-meteo.com/v1/search");q:=u.Query();q.Set("name",loc);q.Set("count","1");q.Set("language",stringArg(a,"language"));q.Set("format","json");u.RawQuery=q.Encode();b,_,_,err:=tier4HTTP(ctx,u.String(),nil);if err!=nil{return nil,err};var g struct{Results []struct{Name string `json:"name"`;Country string `json:"country_code"`;Lat float64 `json:"latitude"`;Lon float64 `json:"longitude"`} `json:"results"`};if err:=json.Unmarshal(b,&g);err!=nil{return nil,err};if len(g.Results)==0{return nil,fmt.Errorf("could not geocode location %q",loc)};lat=g.Results[0].Lat;lon=g.Results[0].Lon;place=g.Results[0].Name;country=g.Results[0].Country}
	u,_:=url.Parse("https://api.open-meteo.com/v1/forecast");q:=u.Query();q.Set("latitude",fmt.Sprint(lat));q.Set("longitude",fmt.Sprint(lon));q.Set("timezone","auto");imperial:=stringArg(a,"units")=="imperial";if imperial{q.Set("temperature_unit","fahrenheit");q.Set("wind_speed_unit","mph");q.Set("precipitation_unit","inch")}
	if name=="weather.current"{q.Set("current","temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,precipitation,weather_code")}else{days:=intArg(a,"days",7);q.Set("daily","temperature_2m_min,temperature_2m_max,precipitation_sum,weather_code");q.Set("forecast_days",fmt.Sprint(days));if boolArg(a,"hourly",false){q.Set("hourly","temperature_2m,precipitation_probability")}}
	u.RawQuery=q.Encode();b,_,_,err:=tier4HTTP(ctx,u.String(),nil);if err!=nil{return nil,err};var data map[string]any;if err:=json.Unmarshal(b,&data);err!=nil{return nil,err};data["source"]="open-meteo";data["location"]=map[string]any{"name":place,"country":country,"lat":lat,"lon":lon};data["units"]=stringArg(a,"units");if data["units"]==""{data["units"]="metric"};if current,ok:=data["current"].(map[string]any);ok{current["condition_text"]=wmoText(numValue(current["weather_code"]))};if daily,ok:=data["daily"].(map[string]any);ok{if codes,ok:=daily["weather_code"].([]any);ok{txt:=[]string{};for _,c:=range codes{txt=append(txt,wmoText(numValue(c)))};daily["condition_text"]=txt}}
	return data,nil
}
func wmoText(n float64)string{switch int(n){case 0:return "Clear";case 1,2,3:return "Partly cloudy";case 45,48:return "Fog";case 51,53,55,56,57,61,63,65,66,67:return "Rain";case 71,73,75,77:return "Snow";case 80,81,82:return "Showers";case 95,96,99:return "Thunderstorm"};return "Unknown"}

func tier4LanguageDetect(a map[string]any)(any,error){
	s:=strings.TrimSpace(stringArg(a,"text"));if s==""{return nil,errors.New("text is empty")}
	lower:=strings.ToLower(s);lang,confidence:="en",0.45
	if regexp.MustCompile(`[\p{Han}]`).MatchString(s){lang,confidence="zh",0.92}else if regexp.MustCompile(`[\p{Hiragana}\p{Katakana}]`).MatchString(s){lang,confidence="ja",0.95}else if regexp.MustCompile(`[\p{Hangul}]`).MatchString(s){lang,confidence="ko",0.95}else if regexp.MustCompile(`[\p{Arabic}]`).MatchString(s){lang,confidence="ar",0.92}else if regexp.MustCompile(`[\p{Cyrillic}]`).MatchString(s){lang,confidence="ru",0.85}else if regexp.MustCompile(`[\p{Devanagari}]`).MatchString(s){lang,confidence="hi",0.9}else{
		sets:=[]struct{code string; words []string}{{"es",[]string{" el "," la "," que "," de "," y "," es "," para "}},{"fr",[]string{" le "," la "," les "," des "," et "," est "," une "}},{"pt",[]string{" o "," a "," que "," de "," e "," não "," para "}},{"de",[]string{" der "," die "," und "," ist "," nicht "," das "}},{"sw",[]string{" na "," ya "," kwa "," ni "," katika "," habari "," lakini "}},{"lg",[]string{" naye "," kubanga "," era "," nnyo "," kino "," ki "}}}
		padded:=" "+lower+" ";best:=0
		for _,set:=range sets{score:=0;for _,w:=range set.words{if strings.Contains(padded,w){score++}};if score>best{best=score;lang=set.code;confidence=0.45+float64(score)*0.08}}
	}
	return map[string]any{"language":lang,"confidence":min(confidence,0.98),"alternatives":[]any{},"source":"fuzecli-heuristic","note":"Heuristic detector; confidence is approximate and should be treated cautiously for short text."},nil
}
func tier4ID()string{var b [12]byte;if _,err:=rand.Read(b[:]);err!=nil{return fmt.Sprintf("item_%d",time.Now().UnixNano())};return hex.EncodeToString(b[:])}
func (rt ToolRuntime) tier4Remember(a map[string]any)(any,error){
	content:=strings.TrimSpace(stringArg(a,"content"));if regexp.MustCompile(`(?i)(api[_ -]?key|password|secret|token|private key)\s*[:=]`).MatchString(content){return nil,errors.New("credential-like content cannot be stored in FuzeCLI memory")}
	s,err:=rt.loadTier4State();if err!=nil{return nil,err};id:="mem_"+tier4ID();now:=time.Now().UTC();scope:=stringArg(a,"scope");if scope==""{scope="project"};kind:=stringArg(a,"kind")
	m:=map[string]any{"memory_id":id,"content":content,"kind":kind,"scope":scope,"tags":stringArray(a["tags"]),"confidence":floatArg(a,"confidence",1),"status":"active","created_at":now.Format(time.RFC3339),"updated_at":now.Format(time.RFC3339)}
	if days:=intArg(a,"ttl_days",0);days>0{m["expires_at"]=now.Add(time.Duration(days)*24*time.Hour).Format(time.RFC3339)}else{m["expires_at"]=nil}
	s.Memories=append(s.Memories,m);if err:=rt.saveTier4State(s);err!=nil{return nil,err};return map[string]any{"memory_id":id,"created_at":m["created_at"],"scope":scope,"kind":kind,"expires_at":m["expires_at"]},nil
}
func floatArg(a map[string]any,k string,def float64)float64{v,ok:=a[k].(float64);if !ok{return def};return v}
func (rt ToolRuntime) tier4ListMemories(a map[string]any)(any,error){
	s,err:=rt.loadTier4State();if err!=nil{return nil,err};kind:=stringArg(a,"kind");scope:=stringArg(a,"scope");tags:=stringArray(a["tags"]);include:=boolArg(a,"include_superseded",false);filtered:=[]map[string]any{};now:=time.Now()
	for _,m:=range s.Memories{if !include&&m["status"]!="active"{continue};if kind!=""&&kind!="any"&&m["kind"]!=kind{continue};if scope!=""&&scope!="any"&&m["scope"]!=scope{continue};if exp,ok:=m["expires_at"].(string);ok{if t,e:=time.Parse(time.RFC3339,exp);e==nil&&now.After(t){continue}};if !containsAll(stringArray(m["tags"]),tags){continue};filtered=append(filtered,m)}
	offset:=intArg(a,"offset",0);limit:=intArg(a,"limit",50);if offset>len(filtered){offset=len(filtered)};end:=min(len(filtered),offset+limit)
	return map[string]any{"memories":filtered[offset:end],"count":len(filtered),"offset":offset,"limit":limit},nil
}
func containsAll(have,want []string)bool{for _,w:=range want{ok:=false;for _,h:=range have{if h==w{ok=true;break}};if !ok{return false}};return true}
func (rt ToolRuntime) tier4Recall(a map[string]any)(any,error){
	s,err:=rt.loadTier4State();if err!=nil{return nil,err};query:=strings.ToLower(stringArg(a,"query"));kind:=stringArg(a,"kind");scope:=stringArg(a,"scope");minConf:=floatArg(a,"min_confidence",0);limit:=intArg(a,"limit",10);now:=time.Now();type scored struct{m map[string]any;score int};all:=[]scored{}
	for _,m:=range s.Memories{if m["status"]!="active"&& !boolArg(a,"include_superseded",false){continue};if kind!=""&&kind!="any"&&m["kind"]!=kind{continue};if scope!=""&&scope!="any"&&m["scope"]!=scope{continue};if numValue(m["confidence"])<minConf{continue};if exp,ok:=m["expires_at"].(string);ok{if t,e:=time.Parse(time.RFC3339,exp);e==nil&&now.After(t){continue}};text:=strings.ToLower(fmt.Sprint(m["content"]));score:=0;for _,term:=range strings.Fields(query){if len(term)>2&&strings.Contains(text,term){score++}};if score>0||query==""{all=append(all,scored{m,score})}}
	sort.SliceStable(all,func(i,j int)bool{return all[i].score>all[j].score});out:=[]map[string]any{};for i,v:=range all{if i>=limit{break};out=append(out,v.m)};return map[string]any{"query":a["query"],"memories":out,"count":len(out)},nil
}
func (rt ToolRuntime) tier4UpdateMemory(a map[string]any)(any,error){
	s,err:=rt.loadTier4State();if err!=nil{return nil,err};id:=stringArg(a,"memory_id");for _,m:=range s.Memories{if m["memory_id"]!=id{continue};if v,ok:=a["confidence"];ok{m["confidence"]=v};if v,ok:=a["tags"];ok{m["tags"]=v};if v,ok:=a["status"];ok{m["status"]=v};if v,ok:=a["superseded_by"];ok{m["superseded_by"]=v};m["updated_at"]=time.Now().UTC().Format(time.RFC3339);if err:=rt.saveTier4State(s);err!=nil{return nil,err};return map[string]any{"updated":true,"memory_id":id},nil};return nil,fmt.Errorf("memory %q was not found",id)
}
func (rt ToolRuntime) tier4Forget(a map[string]any)(any,error){
	s,err:=rt.loadTier4State();if err!=nil{return nil,err};ids:=stringArray(a["memory_ids"]);selected:=map[string]bool{};for _,id:=range ids{selected[id]=true};cutoff:=time.Now().Add(-time.Duration(intArg(a,"older_than_days",0))*24*time.Hour);matches:=[]map[string]any{}
	for _,m:=range s.Memories{ok:=len(ids)==0||selected[fmt.Sprint(m["memory_id"])];if scope:=stringArg(a,"scope");scope!=""&&m["scope"]!=scope{ok=false};if kind:=stringArg(a,"kind");kind!=""&&m["kind"]!=kind{ok=false};if days:=intArg(a,"older_than_days",0);days>0{t,_:=time.Parse(time.RFC3339,fmt.Sprint(m["created_at"]));if t.After(cutoff){ok=false}};if !containsAll(stringArray(m["tags"]),stringArray(a["tags"])){ok=false};if ok{matches=append(matches,m)}}
	dry:=boolArg(a,"dry_run",true);if !dry&&!boolArg(a,"confirm",false){return nil,errors.New("permanent memory deletion requires confirm=true")};if !dry&&len(ids)==0&&len(matches)>5{return nil,errors.New("batch memory deletion over five items requires explicit approval")}
	if dry{return map[string]any{"dry_run":true,"would_forget":len(matches),"memory_ids":memoryIDs(matches),"message":"Preview only; no memories deleted."},nil}
	remove:=map[string]bool{};for _,m:=range matches{remove[fmt.Sprint(m["memory_id"])]=true};kept:=s.Memories[:0];for _,m:=range s.Memories{if !remove[fmt.Sprint(m["memory_id"])]{kept=append(kept,m)}};s.Memories=kept;if err:=rt.saveTier4State(s);err!=nil{return nil,err};return map[string]any{"forgotten":memoryIDs(matches),"count":len(matches),"dry_run":false},nil
}
func memoryIDs(ms []map[string]any)[]string{o:=[]string{};for _,m:=range ms{o=append(o,fmt.Sprint(m["memory_id"]))};return o}
func (rt ToolRuntime) tier4ProfileGet(a map[string]any)(any,error){s,e:=rt.loadTier4State();if e!=nil{return nil,e};out:=map[string]any{"profile":s.Profile};if boolArg(a,"include_memories",false){out["memories"]=s.Memories};return out,nil}
func (rt ToolRuntime) tier4ProfileUpdate(a map[string]any)(any,error){s,e:=rt.loadTier4State();if e!=nil{return nil,e};for k,v:=range a{s.Profile[k]=v};s.Profile["updated_at"]=time.Now().UTC().Format(time.RFC3339);if e:=rt.saveTier4State(s);e!=nil{return nil,e};return map[string]any{"updated":true,"profile":s.Profile},nil}
func (rt ToolRuntime) tier4ProfileReset(a map[string]any)(any,error){s,e:=rt.loadTier4State();if e!=nil{return nil,e};fields:=stringArray(a["fields"]);if len(fields)==0{for k:=range s.Profile{fields=append(fields,k)}};if boolArg(a,"dry_run",true){return map[string]any{"dry_run":true,"fields_to_erase":fields,"message":"Preview only; profile unchanged."},nil};if !boolArg(a,"confirm",false){return nil,errors.New("profile reset requires confirm=true")};for _,k:=range fields{delete(s.Profile,k)};if e:=rt.saveTier4State(s);e!=nil{return nil,e};return map[string]any{"reset":true,"fields_erased":fields},nil}
func (rt ToolRuntime) tier4Feedback(a map[string]any)(any,error){s,e:=rt.loadTier4State();if e!=nil{return nil,e};id:="fb_"+tier4ID();item:=map[string]any{"feedback_id":id,"rating":a["rating"],"target":a["target"],"reason":a["reason"],"tags":a["tags"],"created_at":time.Now().UTC().Format(time.RFC3339)};s.Feedback=append(s.Feedback,item);if e:=rt.saveTier4State(s);e!=nil{return nil,e};return map[string]any{"recorded":true,"feedback_id":id},nil}
