package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

type ClientIPResolver struct {
	trusted []*net.IPNet
}

func NewClientIPResolver(cidrs []string) (*ClientIPResolver, error) {
	resolver := &ClientIPResolver{}
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		resolver.trusted = append(resolver.trusted, network)
	}
	return resolver, nil
}

func (r *ClientIPResolver) Resolve(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !r.isTrusted(peer) {
		return host
	}
	forwarded := req.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return host
	}
	parts := strings.Split(forwarded, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(parts[i]))
		if candidate != nil && !r.isTrusted(candidate) {
			return candidate.String()
		}
	}
	return host
}

func (r *ClientIPResolver) isTrusted(ip net.IP) bool {
	for _, network := range r.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
