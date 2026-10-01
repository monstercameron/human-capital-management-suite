package agentmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	schemaflux "github.com/monstercameron/schemaflux"
)

// TypedModelInput contains SchemaFlux's Go-derived schema and the gateway's
// already redacted skill prompt. It carries no caller-selected model or key.
type TypedModelInput struct {
	Prompt    string
	Schema    json.RawMessage
	WebSearch bool
}

// TypedModelDispatcher resolves current durable task authority and dispatches
// through the owned model gateway, including its budget reservation. It must
// never use a process-default provider or derive authority from Prompt.
type TypedModelDispatcher interface {
	DispatchTypedModel(context.Context, Request, TypedModelInput) (ModelResult, error)
}

type gatewayTypedProvider struct {
	dispatcher  TypedModelDispatcher
	request     Request
	result      ModelResult
	called      bool
	validate    func(json.RawMessage) error
	dispatchErr error
}

func (*gatewayTypedProvider) Name() string                                      { return "openai" }
func (*gatewayTypedProvider) EstimateCost(schemaflux.CompletionRequest) float64 { return 0 }
func (*gatewayTypedProvider) RetryPolicy() (int, time.Duration)                 { return 0, 0 }
func (p *gatewayTypedProvider) Complete(ctx context.Context, completion schemaflux.CompletionRequest) (schemaflux.CompletionResponse, error) {
	if p.called {
		return schemaflux.CompletionResponse{}, ErrRetryLimit
	}
	p.called = true
	schema, err := json.Marshal(completion.JSONSchema)
	if err != nil || len(completion.JSONSchema) == 0 {
		return schemaflux.CompletionResponse{}, ErrInvalidModelRequest
	}
	p.result, err = p.dispatcher.DispatchTypedModel(ctx, p.request, TypedModelInput{Prompt: completion.SystemPrompt + "\n" + completion.UserPrompt, Schema: schema, WebSearch: completion.WebSearch})
	if err != nil {
		if errors.Is(err, ErrBudgetFailed) {
			p.dispatchErr = ErrBudgetFailed
			return schemaflux.CompletionResponse{}, ErrBudgetFailed
		}
		return schemaflux.CompletionResponse{}, fmt.Errorf("agentmodel: configured model dispatch failed")
	}
	if p.result.Failure != nil || p.result.Refusal != nil || p.result.Finish != FinishComplete || !json.Valid(p.result.Structured) {
		return schemaflux.CompletionResponse{}, ErrInvalidModelResult
	}
	if p.validate == nil || p.validate(p.result.Structured) != nil {
		return schemaflux.CompletionResponse{}, ErrInvalidModelResult
	}
	return schemaflux.CompletionResponse{Content: string(p.result.Structured), Model: p.result.Provider.ModelID, Provider: p.result.Provider.ProviderID, FinishReason: "stop", Usage: schemaflux.TokenUsage{InputTokens: int(p.result.Usage.InputTokens), OutputTokens: int(p.result.Usage.OutputTokens), TotalTokens: int(p.result.Usage.TotalTokens)}}, nil
}

func failModelReservation(reservation Reservation) {
	if reservation != nil {
		_ = reservation.Fail()
	}
}

// Guard SchemaFlux's parsing diagnostics from raw provider bytes. The library
// remains responsible for generating and validating the target Go schema.
func validateTypedDispatchOutput[T any](raw json.RawMessage) error {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil {
		return ErrInvalidModelResult
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ErrInvalidModelResult
	}
	typeOf := reflect.TypeOf(value)
	if typeOf != nil && typeOf.Kind() == reflect.Struct {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
			return ErrInvalidModelResult
		}
	}
	return validateTypedRequiredValues(reflect.ValueOf(value), 0)
}

func validateTypedRequiredValues(value reflect.Value, depth int) error {
	if !value.IsValid() || depth > 32 {
		return ErrInvalidModelResult
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if !field.IsExported() || strings.Split(field.Tag.Get("json"), ",")[0] == "-" {
				continue
			}
			current := value.Field(i)
			if strings.EqualFold(strings.TrimSpace(field.Tag.Get("schemaflux")), "required") && current.IsZero() {
				return ErrInvalidModelResult
			}
			if !current.IsZero() {
				if err := validateTypedRequiredValues(current, depth+1); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateTypedRequiredValues(value.Index(i), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
