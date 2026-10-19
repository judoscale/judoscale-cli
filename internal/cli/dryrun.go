package cli

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// printRequest shows a request instead of sending it, with the token masked.
func (a *App) printRequest(req *http.Request, body []byte) error {
	fmt.Fprintf(a.Stdout, "%s %s\n", req.Method, req.URL)
	var names []string
	for name := range req.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := req.Header.Get(name)
		if name == "Authorization" {
			value = "Bearer " + mask(strings.TrimPrefix(value, "Bearer "))
		}
		fmt.Fprintf(a.Stdout, "%s: %s\n", name, value)
	}
	if len(body) > 0 {
		fmt.Fprintln(a.Stdout)
		return a.printJSON(body)
	}
	return nil
}
