package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"onplo.com/go-client/operation"
	"onplo.com/go-client/origin"
)

// client is what the provider's configuration gives every resource: the key it calls with, and
// where the calls go.
type client struct {
	key     string
	origin  string // "" for the contract's own server
	http    *http.Client
	version string
}

// refusal is an answer from Onplo that is not a success.
type refusal struct {
	status int
	text   string
}

func (r *refusal) Error() string { return fmt.Sprintf("Onplo answered %d: %s", r.status, r.text) }

func isGone(err error) bool {
	r, ok := err.(*refusal)
	return ok && r.status == http.StatusNotFound
}

// call sends one operation and reads its answer. path fills the path's parameters, query the
// query's, and body, when not nil, is sent as JSON.
func (c *client) call(ctx context.Context, d operation.Domain, op *operation.Operation,
	path map[string]string, query url.Values, body map[string]any) (map[string]any, error) {
	server, err := origin.Server(d.Server, c.origin)
	if err != nil {
		return nil, fmt.Errorf("api_origin %v", err)
	}
	address := op.Path
	for name, value := range path {
		address = strings.ReplaceAll(address, "{"+name+"}", url.PathEscape(value))
	}
	if strings.Contains(address, "{") {
		return nil, fmt.Errorf("%s: a path parameter is not known: %s", op.ID, address)
	}
	address = server + address
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	var sent io.Reader
	if body != nil {
		content, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		sent = bytes.NewReader(content)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, address, sent)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if sent != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !op.SignedOut {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	req.Header.Set("User-Agent", "terraform-provider-onplo/"+strings.ReplaceAll(c.version, " ", "-"))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s did not reach Onplo: %w", op.ID, err)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: the answer was cut off: %w", op.ID, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		text := strings.TrimSpace(string(answer))
		if len(text) > 4096 {
			text = text[:4096] + "…"
		}
		return nil, &refusal{status: resp.StatusCode, text: text}
	}
	if len(bytes.TrimSpace(answer)) == 0 {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(answer))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%s: Onplo's answer is not a JSON object: %w", op.ID, err)
	}
	return object, nil
}
