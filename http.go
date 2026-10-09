package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/davelawson/grim/api"
)

func newHTTPClient(caCert string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caCert != "" {
		pem, err := os.ReadFile(caCert)
		if err != nil {
			return nil, fmt.Errorf("read CA certificate: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificates: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA certificate file contains no valid certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func execute(ctx context.Context, client *http.Client, o options, out io.Writer) error {
	var body io.Reader
	var method, path string
	var response any
	switch o.command {
	case "login":
		data, err := json.Marshal(api.LoginRequest{Email: o.email, Password: o.password})
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
		method, path, response = api.LoginMethod, api.LoginRoute, &api.LoginResponse{}
	case "match":
		method, path, response = api.GetMatchMethod, api.GetMatchPath(o.id), &api.MatchResponse{}
	case "catalogue":
		method, path, response = api.GetCatalogueMethod, api.GetCataloguePath(o.id), &api.CatalogueResponse{}
	}
	req, err := http.NewRequestWithContext(ctx, method, o.server+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if o.command != "login" {
		req.Header.Set("Authorization", o.token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var failure api.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil && failure.Error != nil {
			return fmt.Errorf("HTTP %d: %s: %s", resp.StatusCode, failure.Error.Code, failure.Error.Message)
		}
		return fmt.Errorf("HTTP %d: unexpected error response", resp.StatusCode)
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("response contains invalid trailing JSON")
	}
	switch result := response.(type) {
	case *api.LoginResponse:
		if result.Token == "" {
			return fmt.Errorf("login response is missing token")
		}
		_, err = fmt.Fprintln(out, result.Token)
		return err
	case *api.MatchResponse:
		if result.Match == nil {
			return fmt.Errorf("match response is missing match")
		}
	case *api.CatalogueResponse:
		if result.Catalogue == nil {
			return fmt.Errorf("catalogue response is missing catalogue")
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(response)
}
