package platform

import("regexp";"strings")

var secretPattern=regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9_\-]{12,}`)
func ReviewDiff(diff string)Review{r:=Review{Passed:true,Summary:"No blocking findings detected by local review rules."};for i,line:=range strings.Split(diff,"\n"){if strings.HasPrefix(line,"+")&&!strings.HasPrefix(line,"+++")&&secretPattern.MatchString(line){r.Findings=append(r.Findings,ReviewFinding{Severity:"blocking",Line:i+1,Message:"Possible credential or secret in changed code."});r.Passed=false};if strings.HasPrefix(line,"+")&&strings.Contains(strings.ToLower(line),"todo"){r.Findings=append(r.Findings,ReviewFinding{Severity:"warning",Line:i+1,Message:"New TODO marker detected."})}};if len(r.Findings)>0{r.Summary="Local review found items that should be inspected before delivery."};return r}
