package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"io"
	"net/http"
)

func integrate1IconRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, input, output any) error {
	switch action {
	case "preview", "regenerate", "shuffle", "reset", "undo":
	default:
		return errPersonaChat
	}
	endpoint, err := personaChatURL(cfg, "/api/agent-controls/icons/"+action, "")
	if err != nil {
		return err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errPersonaChat
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(output)
}
