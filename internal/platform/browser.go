package platform

import("context";"errors";"net";"net/http";"net/url";"strings";"time")

type BrowserSession struct{ID string `json:"id"`;URL string `json:"url"`;Status int `json:"status"`;Duration time.Duration `json:"duration"`}

func InspectURL(ctx context.Context,rawURL string)(BrowserSession,error){
 u,err:=url.Parse(strings.TrimSpace(rawURL));if err!=nil{return BrowserSession{},err}
 if u.Scheme!="http"&&u.Scheme!="https"{return BrowserSession{},errors.New("browser inspection requires http or https")}
 host:=u.Hostname();if host==""{return BrowserSession{},errors.New("browser inspection requires a host")}
 ip:=net.ParseIP(host);if ip!=nil&&!ip.IsLoopback(){return BrowserSession{},errors.New("browser inspection only permits loopback IPs")}
 if ip==nil&&!strings.EqualFold(host,"localhost"){return BrowserSession{},errors.New("browser inspection only permits localhost or loopback hosts")}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if err!=nil{return BrowserSession{},err}
 start:=time.Now();resp,err:=http.DefaultClient.Do(req);if err!=nil{return BrowserSession{},err};defer resp.Body.Close()
 return BrowserSession{ID:time.Now().UTC().Format("20060102T150405.000000000Z"),URL:u.String(),Status:resp.StatusCode,Duration:time.Since(start)},nil
}
