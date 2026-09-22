package mvp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"funroute/extensions/paymentdemo"
	"funroute/lang"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	registry, err := paymentdemo.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestCatalogAPIIncludesExtensionPresentation(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	response := httptest.NewRecorder()
	testServer(t).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var catalog lang.LanguageCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, function := range catalog.Functions {
		if function.Name == "route.is_healthy_v1" {
			found = true
			if function.Doc.Label != "渠道是否健康" || function.Doc.Description == "" {
				t.Fatalf("display = %#v", function.Doc)
			}
		}
	}
	if !found {
		t.Fatal("payment extension is missing from catalog")
	}
}

// A compile error says where: the console puts the caret there instead of
// making an operator hunt through the expression.
func TestCompileErrorsCarryLineAndColumn(t *testing.T) {
	server := testServer(t)
	response := postJSON(t, server, "/api/compile",
		`{"source":"amount\n  + \"x\"","contract":{"args":[{"name":"amount","type":"int"}],"result":{"type":"int"}}}`)
	if response.Code != 422 {
		t.Fatalf("status = %d", response.Code)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Line != 2 || body.Error.Column != 3 {
		t.Fatalf("position = %d:%d, want 2:3 (%s)", body.Error.Line, body.Error.Column, body.Error.Message)
	}
	if strings.Contains(body.Error.Message, "byte") {
		t.Fatalf("the message should not carry a byte offset: %s", body.Error.Message)
	}
}

func TestContractCheckAPIValidatesTheRuntimeABI(t *testing.T) {
	server := testServer(t)
	valid := postJSON(t, server, "/api/contract/check", `{
		"contract":{"args":[{"name":"amount","type":"int"}],"result":{"type":"string"}}
	}`)
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"valid":true`) {
		t.Fatalf("valid status = %d, body = %s", valid.Code, valid.Body.String())
	}
	var checked struct {
		Args   []lang.Parameter `json:"args"`
		Result lang.Type        `json:"result"`
	}
	if err := json.Unmarshal(valid.Body.Bytes(), &checked); err != nil {
		t.Fatal(err)
	}
	if len(checked.Args) != 1 || checked.Args[0].Name != "amount" || !checked.Result.Equal(lang.StringType) {
		t.Fatalf("checked contract = %#v", checked)
	}
	for _, body := range []string{
		`{"contract":{"args":[],"result":null}}`,
		`{"contract":{"args":[{"name":"amount","type":"int"},{"name":"amount","type":"int"}],"result":{"type":"int"}}}`,
		`{"contract":{"args":[{"name":"bad-name","type":"int"}],"result":{"type":"int"}}}`,
	} {
		response := postJSON(t, server, "/api/contract/check", body)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
}

func TestCompileRequiresAndChecksTheRuntimeContract(t *testing.T) {
	server := testServer(t)
	missing := postJSON(t, server, "/api/compile", `{"source":"1"}`)
	if missing.Code != http.StatusUnprocessableEntity || !strings.Contains(missing.Body.String(), "contract is required") {
		t.Fatalf("missing status = %d, body = %s", missing.Code, missing.Body.String())
	}
	mismatch := postJSON(t, server, "/api/compile", `{
		"source":"amount + 1",
		"contract":{"args":[{"name":"amount","type":"int"}],"result":{"type":"string"}}
	}`)
	if mismatch.Code != http.StatusUnprocessableEntity || !strings.Contains(mismatch.Body.String(), "expression returns") {
		t.Fatalf("mismatch status = %d, body = %s", mismatch.Code, mismatch.Body.String())
	}
}

func TestCompileAndRunAPI(t *testing.T) {
	server := testServer(t)
	compileBody := []byte(`{"source":"if(route.is_healthy_v1(health),\"adyen\",\"stripe\")","contract":{"args":[{"name":"health","type":"string"}],"result":{"type":"string"}}}`)
	compileRequest := httptest.NewRequest(http.MethodPost, "/api/compile", bytes.NewReader(compileBody))
	compileRequest.Header.Set("Content-Type", "application/json")
	compileRecorder := httptest.NewRecorder()
	server.ServeHTTP(compileRecorder, compileRequest)
	if compileRecorder.Code != http.StatusOK {
		t.Fatalf("compile status = %d, body = %s", compileRecorder.Code, compileRecorder.Body.String())
	}
	var compiled compileResponse
	if err := json.Unmarshal(compileRecorder.Body.Bytes(), &compiled); err != nil {
		t.Fatal(err)
	}
	if len(compiled.Args) != 1 || compiled.Args[0].Name != "health" || !compiled.Result.Equal(lang.StringType) {
		t.Fatalf("compiled = %#v", compiled)
	}

	runBody := []byte(`{"source":"if(route.is_healthy_v1(health),\"adyen\",\"stripe\")","contract":{"args":[{"name":"health","type":"string"}],"result":{"type":"string"}},"args":{"health":"UP"}}`)
	runRequest := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(runBody))
	runRequest.Header.Set("Content-Type", "application/json")
	runResponse := httptest.NewRecorder()
	server.ServeHTTP(runResponse, runRequest)
	if runResponse.Code != http.StatusOK {
		t.Fatalf("run status = %d, body = %s", runResponse.Code, runResponse.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(runResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["value"] != "adyen" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunAPISupportsFunctionalSwitchAndFor(t *testing.T) {
	server := testServer(t)
	for _, test := range []struct {
		body string
		want any
	}{
		{
			body: `{"source":"switch(country, case \"SG\" => \"adyen\", else \"stripe\")","contract":{"args":[{"name":"country","type":"string"}],"result":{"type":"string"}},"args":{"country":"SG"}}`,
			want: "adyen",
		},
		{
			body: `{"source":"[channel for channel in channels if route.is_healthy_v1(channel)]","contract":{"args":[{"name":"channels","type":"array<string>"}],"result":{"type":"array<string>"}},"args":{"channels":["UP","DOWN","UP"]}}`,
			want: []any{"UP", "UP"},
		},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(result["value"]) != fmt.Sprint(test.want) {
			t.Fatalf("value = %#v, want %#v", result["value"], test.want)
		}
	}
}

func TestRunAPIPreservesEnumContractAndTypedErrors(t *testing.T) {
	server := testServer(t)
	body := `{"source":"switch(channel, case @adyen => @stripe, case @stripe => @adyen)","contract":{"args":[{"name":"channel","type":"enum<channel>{adyen,stripe}"}],"result":{"type":"enum<channel>{adyen,stripe}"}},"args":{"channel":"adyen"}}`
	response := postJSON(t, server, "/api/run", body)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"kind":"enum"`) {
		t.Fatalf("enum run: status = %d, body = %s", response.Code, response.Body.String())
	}
	bad := strings.Replace(body, `"channel":"adyen"`, `"channel":"other"`, 1)
	response = postJSON(t, server, "/api/run", bad)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"CONTRACT_ERROR"`) {
		t.Fatalf("enum error: status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestStaticMVPIsEmbedded(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	testServer(t).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "FunRoute Policy Studio") ||
		!strings.Contains(response.Body.String(), "funroute-designer") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/funroute-dnd.js", nil)
	response = httptest.NewRecorder()
	testServer(t).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "placeBlock") {
		t.Fatalf("drop rules: status = %d, body = %s", response.Code, response.Body.String())
	}
}

func postJSON(t *testing.T, server *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func TestParseAPIReturnsCanonicalExprJSON(t *testing.T) {
	server := testServer(t)
	response := postJSON(t, server, "/api/parse", `{"source":"reduce(price in prices, total = 0, add(total,price))"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var parsed struct {
		ExprJSON json.RawMessage `json:"expr_json"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	// The contract with the front end is the JSON, not a Go type: assert on the
	// document a browser would receive.
	var document struct {
		Version int `json:"version"`
		Expr    struct {
			Node        string `json:"node"`
			Variable    string `json:"variable"`
			Accumulator string `json:"accumulator"`
		} `json:"expr"`
	}
	if err := json.Unmarshal(parsed.ExprJSON, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != lang.ExprJSONVersion || document.Expr.Node != "reduce" {
		t.Fatalf("document = %+v", document)
	}
	if document.Expr.Variable != "price" || document.Expr.Accumulator != "total" {
		t.Fatalf("reduce = %+v", document.Expr)
	}

	// The parse endpoint does not type check; a malformed expression still fails.
	rejected := postJSON(t, server, "/api/parse", `{"source":"add(1,"}`)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rejected.Code, rejected.Body.String())
	}
}

func TestRunAPISupportsReduceAndComprehension(t *testing.T) {
	server := testServer(t)
	for _, test := range []struct {
		name string
		body string
		want any
	}{
		{
			name: "reduce",
			body: `{"source":"reduce(price in prices, total = 0, add(total,price))","contract":{"args":[{"name":"prices","type":"array<int>"}],"result":{"type":"int"}},"args":{"prices":[10,20,30]}}`,
			want: int64(60),
		},
		{
			name: "comprehension",
			body: `{"source":"[add(x,1) for x in items if gt(x,1)]","contract":{"args":[{"name":"items","type":"array<int>"}],"result":{"type":"array<int>"}},"args":{"items":[1,2,3]}}`,
			want: []any{int64(3), int64(4)},
		},
	} {
		response := postJSON(t, server, "/api/run", test.body)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", test.name, response.Code, response.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(result["value"]) != fmt.Sprint(test.want) {
			t.Fatalf("%s: value = %#v, want %#v", test.name, result["value"], test.want)
		}
	}
}

// A console is exactly its registry: a server built on a registry without
// ReduceForm rejects reduce, and its catalog does not advertise it.
func TestServerIsBoundedByItsRegistry(t *testing.T) {
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry)
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, server, "/api/run", `{"source":"reduce(x in items, t = 0, add(t,x))","contract":{"args":[{"name":"items","type":"array<int>"}],"result":{"type":"int"}},"args":{"items":[1]}}`)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "reduce is not enabled") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var catalog lang.LanguageCatalog
	if err := json.Unmarshal(recorder.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	// switch is enabled, reduce is not; the derived forms are always listed.
	listed := map[string]bool{}
	for _, form := range catalog.SpecialForms {
		listed[form.Name] = true
	}
	if !listed["switch"] || !listed["and"] || listed["reduce"] || listed["for"] {
		t.Fatalf("special forms = %v", listed)
	}
}

func TestCatalogAPIListsTheEnabledForms(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	response := httptest.NewRecorder()
	testServer(t).ServeHTTP(response, request)
	var catalog lang.LanguageCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(catalog.SpecialForms))
	for i, form := range catalog.SpecialForms {
		names[i] = form.Name
	}
	want := []string{"switch", "for", "reduce", "let", "and", "or", "not"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("special forms = %v, want %v", names, want)
	}
}
