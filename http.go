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
	var response, payload any
	expectedStatus := http.StatusOK
	switch o.command {
	case "login":
		payload = api.LoginRequest{Email: o.email, Password: o.password}
		method, path, response = api.LoginMethod, api.LoginRoute, &api.LoginResponse{}
	case "match":
		method, path, response = api.GetMatchMethod, api.GetMatchPath(o.id), &api.MatchResponse{}
	case "matches":
		method, path, response = api.ListMatchesMethod, api.ListMatchesRoute, &api.ListMatchesResponse{}
	case "lobbies":
		method, path, response = api.ListLobbiesMethod, api.ListLobbiesRoute, &api.ListLobbiesResponse{}
	case "public-lobbies":
		method, path, response = api.ListPublicLobbiesMethod, api.ListPublicLobbiesRoute, &api.ListPublicLobbiesResponse{}
	case "lobby":
		method, path, response = api.GetLobbyMethod, api.GetLobbyPath(o.id), &api.GetLobbyResponse{}
	case "lobby-create":
		method, path, response = api.CreateLobbyMethod, api.CreateLobbyRoute, &api.CreateLobbyResponse{}
		payload = api.CreateLobbyRequest{Name: o.name}
	case "lobby-join":
		method, path, response = api.JoinLobbyMethod, api.JoinLobbyPath(o.id), &api.GetLobbyResponse{}
	case "lobby-ready":
		method, path, response = api.SetReadyMethod, api.SetReadyPath(o.id), &api.GetLobbyResponse{}
		payload = api.ReadyRequest{Ready: &o.ready}
	case "lobby-launch":
		method, path, response = api.LaunchMatchMethod, api.LaunchMatchPath(o.id), &api.MatchResponse{}
		payload = api.LaunchRequest{RequestID: o.id}
		expectedStatus = http.StatusCreated
	case "lobby-leave":
		method, path = api.LeaveLobbyMethod, api.LeaveLobbyPath(o.id)
	case "lobby-remove-user":
		method, path = api.RemoveUserFromLobbyMethod, api.RemoveUserFromLobbyPath(o.id, o.userID)
	case "lobby-delete":
		method, path = api.DeleteLobbyMethod, api.DeleteLobbyPath(o.id)
	case "catalogue":
		method, path, response = api.GetCatalogueMethod, api.GetCataloguePath(o.id), &api.CatalogueResponse{}
	}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
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
	if resp.StatusCode != expectedStatus {
		var failure api.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil && failure.Error != nil {
			return fmt.Errorf("HTTP %d: %s: %s", resp.StatusCode, failure.Error.Code, failure.Error.Message)
		}
		return fmt.Errorf("HTTP %d: unexpected error response", resp.StatusCode)
	}
	if response == nil {
		return nil
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
	case *api.ListMatchesResponse:
		if result.Matches == nil {
			return fmt.Errorf("matches response is missing matches array")
		}
	case *api.ListLobbiesResponse:
		if result.Lobbies == nil {
			return fmt.Errorf("lobbies response is missing lobbies array")
		}
	case *api.ListPublicLobbiesResponse:
		if result.Lobbies == nil {
			return fmt.Errorf("public lobbies response is missing lobbies array")
		}
		for _, lobby := range result.Lobbies {
			if lobby.ID == "" {
				return fmt.Errorf("public lobbies response is missing lobby id")
			}
		}
	case *api.CreateLobbyResponse:
		if result.Id == "" {
			return fmt.Errorf("create lobby response is missing id")
		}
	case *api.GetLobbyResponse:
		if result.Lobby.Id == "" {
			return fmt.Errorf("lobby response is missing lobby or id")
		}
	case *api.MatchResponse:
		if result.Match == nil || result.Match.ID == "" {
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
