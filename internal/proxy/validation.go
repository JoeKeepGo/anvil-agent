package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

func validateProxyRequest(req incus.ProxyRequest) error {
	if req.ID == "" {
		return fmt.Errorf("missing id")
	}
	if req.Method == "" {
		return fmt.Errorf("missing method")
	}
	if !isAllowedMethod(req.Method) {
		return fmt.Errorf("unsupported method")
	}
	if req.Path == "" {
		return fmt.Errorf("missing path")
	}
	if req.Path == "/agent/v1/state" || strings.HasPrefix(req.Path, "/agent/v1/") {
		return nil
	}
	if req.Path != "/1.0" && !strings.HasPrefix(req.Path, "/1.0/") {
		return fmt.Errorf("path outside incus api")
	}
	if req.Method != http.MethodGet {
		return fmt.Errorf("incus write method not allowed")
	}
	if !isAllowedIncusReadPath(req.Path) {
		return fmt.Errorf("incus read path not allowed")
	}
	return nil
}

func isAllowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isAllowedIncusReadPath(rawPath string) bool {
	parsed, err := url.ParseRequestURI(rawPath)
	if err != nil {
		return false
	}

	switch parsed.Path {
	case "/1.0", "/1.0/instances":
		return parsed.RawQuery == ""
	case "/1.0/images", "/1.0/operations":
		return parsed.RawQuery == "" || isRecursionOneQuery(parsed.Query())
	}

	for _, prefix := range []string{"/1.0/instances/", "/1.0/images/", "/1.0/operations/"} {
		if strings.HasPrefix(parsed.Path, prefix) && len(parsed.Path) > len(prefix) {
			return parsed.RawQuery == ""
		}
	}

	return false
}

func isRecursionOneQuery(values url.Values) bool {
	return len(values) == 1 && len(values["recursion"]) == 1 && values.Get("recursion") == "1"
}
