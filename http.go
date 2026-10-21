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
	"strings"
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
	case "play-card", "attack", "challenge", "activate", "research-element", "research-spell", "heal-with-focus", "end-turn", "maintenance", "kneel", "resolve-effect", "skip-effect", "undertake-hunt", "choose-research", "end-discard", "hand-discard", "asset-loss", "scry", "react", "defend", "pass":
		method, response = "POST", &api.MatchResponse{}
		path, payload = gameplayPayload(o)
	case "login":
		payload = api.LoginRequest{Email: o.email, Password: o.password}
		method, path, response = api.Login.Method, api.Login.Route, &api.LoginResponse{}
	case "draft":
		method, path, response = api.GetDraft.Method, api.GetDraftPath(o.id), &api.DraftResponse{}
	case "draft-pick":
		method, path, response = api.SubmitDraft.Method, api.SubmitDraftPath(o.id), &api.DraftResponse{}
		payload = api.DraftPickRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, Type: o.choiceType, CardID: o.cardID}
	case "match":
		method, path, response = api.GetMatch.Method, api.GetMatchPath(o.id), &api.MatchResponse{}
	case "matches":
		method, path, response = api.ListMatches.Method, api.ListMatches.Route, &api.ListMatchesResponse{}
	case "lobbies":
		method, path, response = api.ListLobbies.Method, api.ListLobbies.Route, &api.ListLobbiesResponse{}
	case "public-lobbies":
		method, path, response = api.ListPublicLobbies.Method, api.ListPublicLobbies.Route, &api.ListPublicLobbiesResponse{}
	case "lobby":
		method, path, response = api.GetLobby.Method, api.GetLobbyPath(o.id), &api.GetLobbyResponse{}
	case "lobby-create":
		method, path, response = api.CreateLobby.Method, api.CreateLobby.Route, &api.CreateLobbyResponse{}
		payload = api.CreateLobbyRequest{Name: o.name}
	case "lobby-join":
		method, path, response = api.JoinLobby.Method, api.JoinLobbyPath(o.id), &api.GetLobbyResponse{}
	case "lobby-add-bot":
		method, path, response = api.AddBotToLobby.Method, api.AddBotToLobbyPath(o.id), &api.GetLobbyResponse{}
		expectedStatus = http.StatusCreated
	case "lobby-remove-bot":
		method, path, response = api.RemoveBotFromLobby.Method, api.RemoveBotFromLobbyPath(o.id, o.botID), &api.GetLobbyResponse{}
	case "lobby-ready":
		method, path, response = api.SetReady.Method, api.SetReadyPath(o.id), &api.GetLobbyResponse{}
		payload = api.ReadyRequest{Ready: &o.ready}
	case "lobby-launch":
		method, path, response = api.LaunchMatch.Method, api.LaunchMatchPath(o.id), &api.MatchResponse{}
		payload = api.LaunchRequest{RequestID: o.id}
		expectedStatus = http.StatusCreated
	case "lobby-leave":
		method, path = api.LeaveLobby.Method, api.LeaveLobbyPath(o.id)
	case "lobby-remove-user":
		method, path = api.RemoveUserFromLobby.Method, api.RemoveUserFromLobbyPath(o.id, o.userID)
	case "lobby-delete":
		method, path = api.DeleteLobby.Method, api.DeleteLobbyPath(o.id)
	case "catalogue":
		method, path, response = api.GetCatalogue.Method, api.GetCataloguePath(o.id), &api.CatalogueResponse{}
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
	// Pointer fields distinguish missing/null values from valid revision zero.
	var target any = response
	var draftWire struct {
		Draft    *api.Draft       `json:"draft"`
		Cards    []api.CardDesign `json:"cards"`
		Revision *int64           `json:"revision"`
		Status   *string          `json:"status"`
	}
	if _, ok := response.(*api.DraftResponse); ok {
		target = &draftWire
	}
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("response contains invalid trailing JSON")
	}
	switch result := response.(type) {
	case *api.DraftResponse:
		if draftWire.Draft == nil || draftWire.Draft.Step == "" {
			return fmt.Errorf("draft response is missing draft progress")
		}
		if draftWire.Revision == nil || *draftWire.Revision < 0 {
			return fmt.Errorf("draft response is missing or invalid revision")
		}
		if draftWire.Status == nil {
			return fmt.Errorf("draft response is missing status")
		}
		switch *draftWire.Status {
		case "setup", "playing", "finished":
		default:
			return fmt.Errorf("draft response has invalid status")
		}
		if draftWire.Cards == nil {
			return fmt.Errorf("draft response is missing cards array")
		}
		switch draftWire.Draft.Step {
		case "chantry", "victory_path", "minion":
			if *draftWire.Status == "setup" && len(draftWire.Cards) == 0 {
				return fmt.Errorf("draft response is missing offered cards")
			}
		case "complete":
			if len(draftWire.Cards) != 0 {
				return fmt.Errorf("completed draft response contains cards")
			}
		default:
			return fmt.Errorf("draft response has invalid step")
		}
		seen := make(map[string]bool)
		for _, card := range draftWire.Cards {
			if strings.TrimSpace(card.ID) == "" || strings.TrimSpace(card.Name) == "" || seen[card.ID] {
				return fmt.Errorf("draft response has invalid card")
			}
			seen[card.ID] = true
		}
		*result = api.DraftResponse{Draft: *draftWire.Draft, Cards: draftWire.Cards, Revision: *draftWire.Revision, Status: *draftWire.Status}
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
