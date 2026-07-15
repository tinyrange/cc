package ccvmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/vm"
)

func TestOpenAPIContract(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "openapi.json")
	loader := openapi3.NewLoader()
	document, err := loader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load OpenAPI document: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI document: %v", err)
	}
	for name, value := range map[string]any{
		"ErrorResponse":           client.ErrorResponse{},
		"CreateInstanceRequest":   client.CreateInstanceRequest{},
		"StartInstanceRequest":    client.StartInstanceRequest{},
		"RunRequest":              client.RunRequest{},
		"ExecResponse":            client.ExecResponse{},
		"ExecRequest":             client.ExecRequest{},
		"ExecInput":               client.ExecInput{},
		"ExecEvent":               client.ExecEvent{},
		"PortForward":             client.PortForward{},
		"ServiceProxyPortRequest": client.ServiceProxyPortRequest{},
		"BootEvent":               client.BootEvent{},
	} {
		assertSchemaFields(t, document, name, reflect.TypeOf(value))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode OpenAPI route inventory: %v", err)
	}

	_, registered := newMuxWithRoutes(
		&server{vms: vm.NewManagerWithHost(nil)}, nil, func() {}, ServerOptions{},
	)
	wantRoutes := make(map[string]struct{}, len(registered))
	for _, route := range registered {
		key := route.Method + " " + route.Path
		if _, duplicate := wantRoutes[key]; duplicate {
			t.Fatalf("duplicate registered API route %s", key)
		}
		wantRoutes[key] = struct{}{}
	}

	documentedRoutes := make(map[string]struct{}, len(wantRoutes))
	for path, item := range raw.Paths {
		for method := range item {
			if !openAPIMethod(method) {
				continue
			}
			documentedRoutes[strings.ToUpper(method)+" "+path] = struct{}{}
		}
	}
	if missing, extra := routeDifference(wantRoutes, documentedRoutes), routeDifference(documentedRoutes, wantRoutes); len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("OpenAPI route drift: missing=%v extra=%v", missing, extra)
	}

	assertNDJSONContract(t, raw.Paths, "POST", "/kernel/download")
	assertNDJSONContract(t, raw.Paths, "POST", "/image/{image}")
	assertNDJSONContract(t, raw.Paths, "POST", "/image/{image}/qemu/download")
	assertNDJSONContract(t, raw.Paths, "POST", "/vm/start")
	assertNDJSONContract(t, raw.Paths, "POST", "/vm")
	assertNDJSONContract(t, raw.Paths, "POST", "/vm/run")
	assertWebSocketContract(t, raw.Paths, "/vm/run", "#/components/schemas/ExecRequest")
	assertWebSocketContract(t, raw.Paths, "/vm/run/stream", "#/components/schemas/RunRequest")
}

func assertSchemaFields(t *testing.T, document *openapi3.T, name string, typ reflect.Type) {
	t.Helper()
	ref, ok := document.Components.Schemas[name]
	if !ok {
		t.Fatalf("OpenAPI schema %s is missing", name)
	}
	documented := make(map[string]struct{})
	collectSchemaFields(ref, documented)
	actual := make(map[string]struct{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			name = field.Name
		}
		if name != "-" {
			actual[name] = struct{}{}
		}
	}
	if missing, extra := routeDifference(actual, documented), routeDifference(documented, actual); len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("OpenAPI schema %s field drift: missing=%v extra=%v", name, missing, extra)
	}
}

func collectSchemaFields(ref *openapi3.SchemaRef, fields map[string]struct{}) {
	if ref == nil || ref.Value == nil {
		return
	}
	for name := range ref.Value.Properties {
		fields[name] = struct{}{}
	}
	for _, part := range ref.Value.AllOf {
		collectSchemaFields(part, fields)
	}
}

func openAPIMethod(method string) bool {
	switch method {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return true
	default:
		return false
	}
}

func routeDifference(left, right map[string]struct{}) []string {
	var difference []string
	for route := range left {
		if _, ok := right[route]; !ok {
			difference = append(difference, route)
		}
	}
	sort.Strings(difference)
	return difference
}

func assertNDJSONContract(t *testing.T, paths map[string]map[string]json.RawMessage, method, path string) {
	t.Helper()
	operation := decodeOperation(t, paths, method, path)
	responses := objectField(t, operation, "responses")
	ok := objectField(t, responses, "200")
	content := objectField(t, ok, "content")
	if _, exists := content["application/x-ndjson"]; !exists {
		t.Fatalf("%s %s does not document application/x-ndjson", method, path)
	}
}

func assertWebSocketContract(t *testing.T, paths map[string]map[string]json.RawMessage, path, initialRef string) {
	t.Helper()
	operation := decodeOperation(t, paths, "GET", path)
	extension := objectField(t, operation, "x-websocket")
	for field, want := range map[string]string{
		"initialClientMessage": initialRef,
		"clientMessages":       "#/components/schemas/ExecInput",
		"serverMessages":       "#/components/schemas/ExecEvent",
	} {
		schema := objectField(t, extension, field)
		if got, _ := schema["$ref"].(string); got != want {
			t.Fatalf("GET %s x-websocket.%s ref = %q, want %q", path, field, got, want)
		}
	}
}

func decodeOperation(t *testing.T, paths map[string]map[string]json.RawMessage, method, path string) map[string]any {
	t.Helper()
	item, ok := paths[path]
	if !ok {
		t.Fatalf("OpenAPI path %s is missing", path)
	}
	raw, ok := item[strings.ToLower(method)]
	if !ok {
		t.Fatalf("OpenAPI operation %s %s is missing", method, path)
	}
	var operation map[string]any
	if err := json.Unmarshal(raw, &operation); err != nil {
		t.Fatalf("decode OpenAPI operation %s %s: %v", method, path, err)
	}
	return operation
}

func objectField(t *testing.T, object map[string]any, field string) map[string]any {
	t.Helper()
	value, ok := object[field].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI field %s is missing or not an object in %s", field, fmt.Sprint(object))
	}
	return value
}
