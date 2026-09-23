package platform

import("context";"errors";"net/http";"time")
type BrowserSession struct{ID string `json:"id"`;URL string `json:"url"`;Status int `json:"status"`;Duration time.Duration `json:"duration"`}
func InspectURL(ctx context.Context,rawURL string)(BrowserSession,error){if rawURL==""{return BrowserSession{},errors.New("url is required")};req,err:=http.NewRequestWithContext(ctx,http.MethodGet,rawURL,nil);if err!=nil{return BrowserSession{},err};start:=time.Now();resp,err:=http.DefaultClient.Do(req);if err!=nil{return BrowserSession{},err};defer resp.Body.Close();return BrowserSession{ID:time.Now().UTC().Format("20060102T150405.000000000Z"),URL:rawURL,Status:resp.StatusCode,Duration:time.Since(start)},nil}
