package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// session drives a server the way a client does and keeps what it sent back.
type session struct {
	t      *testing.T
	server *Server
	out    []map[string]any
	nextID int
}

func newSession(t *testing.T, registry *machine.Registry, capabilities string) *session {
	s := &session{t: t}
	s.server = New(registry, func(message []byte) {
		var decoded map[string]any
		if err := json.Unmarshal(message, &decoded); err != nil {
			t.Fatalf("the server sent %s: %v", message, err)
		}
		s.out = append(s.out, decoded)
	})
	s.request("initialize", json.RawMessage(capabilities))
	return s
}

func (s *session) request(method string, params any) any {
	s.t.Helper()
	s.nextID++
	s.send(map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params})
	for _, message := range s.out {
		if id, ok := message["id"].(float64); ok && int(id) == s.nextID {
			if failure, failed := message["error"]; failed {
				return failure
			}
			return message["result"]
		}
	}
	s.t.Fatalf("%s got no response", method)
	return nil
}

func (s *session) notify(method string, params any) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *session) send(message any) {
	encoded, err := json.Marshal(message)
	if err != nil {
		s.t.Fatal(err)
	}
	s.server.Handle(encoded)
}

func (s *session) open(uri, text string) {
	s.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": text}})
}

// diagnostics is what was last published for uri.
func (s *session) diagnostics(uri string) []any {
	for i := len(s.out) - 1; i >= 0; i-- {
		params, _ := s.out[i]["params"].(map[string]any)
		if s.out[i]["method"] == "textDocument/publishDiagnostics" && params["uri"] == uri {
			return params["diagnostics"].([]any)
		}
	}
	s.t.Fatalf("nothing was published for %s", uri)
	return nil
}

func standard(t *testing.T) *machine.Registry {
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

func contract(args ...string) map[string]any {
	var declared []map[string]string
	for _, arg := range args {
		name, typ, _ := strings.Cut(arg, ":")
		declared = append(declared, map[string]string{"name": name, "type": typ, "doc": name + " 的说明"})
	}
	return map[string]any{"contract": map[string]any{"args": declared}}
}

func at(line, character int) map[string]int {
	return map[string]int{"line": line, "character": character}
}

func position(uri string, line, character int) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": at(line, character)}
}

func docParams(uri string) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}}
}

func TestInitializeAgreesOnAnEncoding(t *testing.T) {
	utf8 := newSession(t, standard(t), `{"capabilities":{"general":{"positionEncodings":["utf-16","utf-8"]}}}`)
	if got := utf8.out[0]["result"].(map[string]any)["capabilities"].(map[string]any)["positionEncoding"]; got != "utf-8" {
		t.Errorf("offered utf-8, agreed on %v", got)
	}
	utf16 := newSession(t, standard(t), `{}`)
	if got := utf16.out[0]["result"].(map[string]any)["capabilities"].(map[string]any)["positionEncoding"]; got != "utf-16" {
		t.Errorf("offered nothing, agreed on %v", got)
	}
}

// A diagnostic covers what it is about, and a read of an argument the
// contract does not declare points at the read.
func TestDiagnosticsFollowTheText(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int", "x:int"))
	s.open("file:///a.fr", "let(rate = fee * 2, rate + x)")
	if got := s.diagnostics("file:///a.fr"); len(got) != 0 {
		t.Fatalf("a correct program has diagnostics: %v", got)
	}
	s.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": "file:///a.fr", "version": 2},
		"contentChanges": []map[string]string{{"text": "let(rate = fee * 2,\n  rate + y)"}},
	})
	got := s.diagnostics("file:///a.fr")
	if len(got) != 1 {
		t.Fatalf("diagnostics %v", got)
	}
	diagnostic := got[0].(map[string]any)
	want := map[string]any{"start": map[string]any{"line": 1.0, "character": 9.0}, "end": map[string]any{"line": 1.0, "character": 10.0}}
	if fmt.Sprint(diagnostic["range"]) != fmt.Sprint(want) || !strings.Contains(diagnostic["message"].(string), `"y"`) {
		t.Errorf("the undeclared read is reported as %v", diagnostic)
	}
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{{"name": "fee", "type": "nope"}}}})
	if got := s.diagnostics("file:///a.fr"); len(got) != 1 || !strings.Contains(fmt.Sprint(got), "argument") {
		t.Errorf("a broken contract is reported as %v", got)
	}
}

// Semantic tokens say what each piece is: the contract's arguments are
// parameters, a local is a variable declared where it is bound.
func TestSemanticTokensNameEachPiece(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "let(rate = fee, rate + 1) // why")
	data := s.request("textDocument/semanticTokens/full", docParams("file:///a.fr")).(map[string]any)["data"].([]any)
	var got []string
	for i := 0; i+4 < len(data); i += 5 {
		name := tokenTypes[int(data[i+3].(float64))]
		if data[i+4].(float64) == 1 {
			name += "+declaration"
		}
		got = append(got, name)
	}
	want := "keyword variable+declaration operator parameter variable operator number comment"
	if strings.Join(got, " ") != want {
		t.Errorf("tokens are %s\nwant       %s", strings.Join(got, " "), want)
	}
}

func TestFormattingAndHover(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int", "x:int"))
	s.open("file:///a.fr", "let(rate=fee*2,rate+x)\n")
	edits := s.request("textDocument/formatting", docParams("file:///a.fr")).([]any)
	if len(edits) != 1 || edits[0].(map[string]any)["newText"] != "let(rate = fee * 2, rate + x)\n" {
		t.Errorf("formatting gives %v", edits)
	}
	shown := s.request("textDocument/hover", position("file:///a.fr", 0, 19)).(map[string]any)
	value := shown["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(value, "rate+x: int") || !strings.Contains(value, "add(int,int)->int") {
		t.Errorf("hover on + is %q", value)
	}
	argument := s.request("textDocument/hover", position("file:///a.fr", 0, 10)).(map[string]any)
	if value := argument["contents"].(map[string]any)["value"].(string); !strings.Contains(value, "fee 的说明") {
		t.Errorf("hover on an argument is %q", value)
	}
}

func labels(items any) []string {
	var out []string
	for _, item := range items.([]any) {
		out = append(out, item.(map[string]any)["label"].(string))
	}
	return out
}

func TestCompletionOffersWhatThePositionCanName(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
		{"name": "fee", "type": "int"}, {"name": "ch", "type": "enum<channel>{adyen,stripe}"},
	}}})
	s.open("file:///a.fr", "let(rate = fee, rate)")
	got := strings.Join(labels(s.request("textDocument/completion", position("file:///a.fr", 0, 17))), " ")
	for _, want := range []string{"fee", "ch", "rate", "add", "let", "switch"} {
		if !strings.Contains(" "+got+" ", " "+want+" ") {
			t.Errorf("completion lacks %s: %s", want, got)
		}
	}
	s.open("file:///b.fr", "ch == @")
	if got := labels(s.request("textDocument/completion", position("file:///b.fr", 0, 7))); strings.Join(got, ",") != "adyen,stripe" {
		t.Errorf("after @ the completion is %v", got)
	}
}

// Signature help works on a call still being typed, which does not parse.
func TestSignatureHelpFindsTheCallBeingTyped(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "if(a, [1, 2], ")
	help := s.request("textDocument/signatureHelp", position("file:///a.fr", 0, 14)).(map[string]any)
	if help["activeParameter"].(float64) != 2 || !strings.HasPrefix(help["signatures"].([]any)[0].(map[string]any)["label"].(string), "if(") {
		t.Errorf("signature help is %v", help)
	}
}

// A record's colon or a field's dot inside an earlier argument does not
// reset the count.
func TestSignatureHelpCountsPastRecordsAndFields(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	for text, want := range map[string]float64{"if(true, {a: 1}, ": 2, "if(r.a, ": 1, "if(true, [x for x in xs], ": 2} {
		s.open("file:///a.fr", text)
		help := s.request("textDocument/signatureHelp", position("file:///a.fr", 0, len(text))).(map[string]any)
		if help["activeParameter"].(float64) != want {
			t.Errorf("%q: the active argument is %v, want %v", text, help["activeParameter"], want)
		}
	}
}

func TestSyntaxTreeIsInClientPositions(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "let(rate = 2,\n  rate + x)")
	tree := s.request("funroute/syntaxTree", docParams("file:///a.fr")).(map[string]any)
	body := tree["fields"].([]any)[1].(map[string]any)["nodes"].([]any)[0].(map[string]any)
	if body["operator"] != "+" || fmt.Sprint(body["range"]) != "map[end:map[character:10 line:1] start:map[character:2 line:1]]" {
		t.Errorf("the body is %v", body)
	}
	s.open("file:///b.fr", "let(x = 1)")
	if got := s.request("funroute/syntaxTree", docParams("file:///b.fr")); got != nil {
		t.Errorf("a program that does not parse has a tree: %v", got)
	}
}

// A run in a runtime that has a host function only as its signature says so.
func TestRunReportsWhatCouldNotRun(t *testing.T) {
	host := standard(t)
	doc := machine.Doc{Label: "风险分", Cost: 25, Params: []string{"国家"}}
	if err := machine.Logic(host, "risk.score_v1", doc, func(string) (float64, error) { return 0.9, nil }); err != nil {
		t.Fatal(err)
	}
	signatures := standard(t)
	if err := host.Manifest().Apply(signatures); err != nil {
		t.Fatal(err)
	}
	s := newSession(t, signatures, `{}`)
	s.notify("funroute/setContract", contract("country:string"))
	s.open("file:///a.fr", "fallback(risk.score_v1(country), 0.5)")
	result := s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///a.fr", "args": map[string]string{"country": "SG"}},
	}}).(map[string]any)
	if result["value"] != 0.5 || fmt.Sprint(result["unavailable"]) != "[risk.score_v1]" {
		t.Errorf("the run is %v", result)
	}
	s = newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("n:int"))
	s.open("file:///b.fr", "n + 1")
	large := s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///b.fr", "args": `{"n": 9007199254740993}`},
	}}).(map[string]any)
	if large["error"] != nil || fmt.Sprintf("%.0f", large["value"]) != "9007199254740994" {
		t.Errorf("args given as text lost digits: %v", large)
	}
}

// Positions count UTF-16 code units unless the client agreed to UTF-8.
func TestPositionsCountTheAgreedUnits(t *testing.T) {
	text := `{"好😀": nope(x)}`
	for capabilities, want := range map[string]float64{`{}`: 8, `{"capabilities":{"general":{"positionEncodings":["utf-8"]}}}`: 12} {
		s := newSession(t, standard(t), capabilities)
		s.open("file:///a.fr", text)
		got := s.diagnostics("file:///a.fr")
		start := got[0].(map[string]any)["range"].(map[string]any)["start"].(map[string]any)["character"]
		if start != want {
			t.Errorf("%s: the error starts at %v, want %v", capabilities, start, want)
		}
	}
	for _, line := range []int{-1, -5} {
		if got := newDocument("x", 0, "ab\ncd").offset(Position{Line: line, Character: 1}, utf16Encoding); got != 1 {
			t.Errorf("line %d names byte %d, want 1", line, got)
		}
	}
	invalid := newDocument("x", 0, "a\xffb")
	for _, encoding := range []string{utf8Encoding, utf16Encoding} {
		if back := invalid.offset(invalid.position(2, encoding), encoding); back != 2 {
			t.Errorf("%s: past an invalid byte, offset 2 comes back as %d", encoding, back)
		}
	}
	doc := newDocument("x", 0, "a😀b\nc")
	for _, offset := range []int{0, 1, 5, 6, 7, 8} {
		if back := doc.offset(doc.position(offset, utf16Encoding), utf16Encoding); back != offset {
			t.Errorf("offset %d comes back as %d", offset, back)
		}
	}
}

func TestServeFramesTheProtocolOnAStream(t *testing.T) {
	var input strings.Builder
	for _, message := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"no/such"}`,
		`{"jsonrpc":"2.0","id":3,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
		`{"jsonrpc":"2.0","id":4,"method":"shutdown"}`,
	} {
		fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n%s", len(message), message)
	}
	reader, writer := io.Pipe()
	go func() {
		_ = Serve(strings.NewReader(input.String()), writer, standard(t))
		writer.Close()
	}()
	var ids []string
	frames := bufio.NewReader(reader)
	for {
		message, err := readFrame(frames)
		if err != nil {
			break
		}
		var decoded map[string]any
		_ = json.Unmarshal(message, &decoded)
		ids = append(ids, fmt.Sprint(decoded["id"], decoded["error"] != nil))
	}
	if strings.Join(ids, " ") != "1 false 2 true 3 false" {
		t.Errorf("the stream answered %v; nothing after exit", ids)
	}
}

func TestRenderAndCatalog(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int"))
	s.open("file:///a.fr", "fee * 2")
	rendered := s.request("workspace/executeCommand", map[string]any{"command": renderCommand, "arguments": []map[string]any{{"uri": "file:///a.fr"}}}).(map[string]any)
	if got := rendered["source"].(string); !strings.HasPrefix(got, "// fee: int") || !strings.HasSuffix(got, "fee * 2") {
		t.Errorf("the rendered rule is %q", got)
	}
	s.notify("funroute/setContract", contract("fee:nope"))
	refused := s.request("workspace/executeCommand", map[string]any{"command": renderCommand, "arguments": []map[string]any{{"uri": "file:///a.fr"}}}).(map[string]any)
	if !strings.Contains(fmt.Sprint(refused["message"]), "nope") {
		t.Errorf("a refused contract is rendered as %v", refused)
	}
	catalog := s.request("funroute/catalog", map[string]any{}).(map[string]any)
	if len(catalog["functions"].([]any)) == 0 || len(catalog["special_forms"].([]any)) == 0 {
		t.Errorf("the catalog is %v", catalog)
	}
}

// A program being typed rarely parses, and the name being typed is rarely
// declared yet; the locals it can see are offered all the same.
func TestCompletionOffersLocalsWhileTyping(t *testing.T) {
	cases := map[string][]string{
		"let(rate = fee, ra":               {"rate"},
		"let(rate = fee, ":                 {"rate"},
		"[x * 2 for x in xs if ":           {"x"},
		"reduce(p in ps, acc = 0, ":        {"p", "acc"},
		"let(a = 1, b = ":                  {"a"},
		"{k: v for k, v in d if ":          {"k", "v"},
		"let(a = 1, switch(case a > 1 => ": {"a"},
		"[x for x in ":                     {},
	}
	for text, want := range cases {
		s := newSession(t, standard(t), `{}`)
		s.notify("funroute/setContract", contract("fee:int"))
		s.open("file:///a.fr", text)
		got := labels(s.request("textDocument/completion", position("file:///a.fr", 0, len(text))))
		for _, name := range want {
			if !strings.Contains(" "+strings.Join(got, " ")+" ", " "+name+" ") {
				t.Errorf("%q: completion lacks the local %s: %v", text, name, got)
			}
		}
		if len(want) == 0 && strings.Contains(" "+strings.Join(got, " ")+" ", " x ") {
			t.Errorf("%q: a loop's source cannot see its variable: %v", text, got)
		}
	}
}

// With no contract the program's arguments are what the text reads: they are
// offered in completion and listed by funroute/arguments, in order.
func TestInferredArgumentsAreOfferedAndListed(t *testing.T) {
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "amount * bps / 10000")
	list := s.request("funroute/arguments", docParams("file:///a.fr")).([]any)
	if len(list) != 2 || list[0].(map[string]any)["name"] != "amount" || list[1].(map[string]any)["type"] != "int" {
		t.Fatalf("the arguments are %v", list)
	}
	if got := labels(s.request("textDocument/completion", position("file:///a.fr", 0, 0))); !slices.Contains(got, "bps") {
		t.Errorf("completion without a contract offers %v", got)
	}
}

// In a record update, where a field name goes, the fields of the record being
// updated are offered — the base's, whether it is an argument or a local —
// without the ones already written.
func TestCompletionOffersTheFieldsOfARecordBeingUpdated(t *testing.T) {
	cases := map[string]string{
		"{...order, ":                    "amount fee currency",
		"{...order, fee: 0, ":            "amount currency",
		"{...order, cu":                  "amount fee currency",
		"let(o = order, {...o, ":         "amount fee currency",
		"{...order, amount: order.fee, ": "fee currency",
	}
	for text, want := range cases {
		s := newSession(t, standard(t), `{}`)
		s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
			{"name": "order", "type": "record{amount: int, fee: int, currency: string}"},
		}}})
		s.open("file:///a.fr", text)
		if got := strings.Join(labels(s.request("textDocument/completion", position("file:///a.fr", 0, len(text)))), " "); got != want {
			t.Errorf("%q: completion is %q, want %q", text, got, want)
		}
	}
	// A declared result the half-written program does not return yet does not
	// hide the base's fields.
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{
		"args":   []map[string]string{{"name": "order", "type": "record{amount: int, fee: int}"}},
		"result": map[string]string{"type": "int"},
	}})
	s.open("file:///a.fr", "{...order, ")
	if got := strings.Join(labels(s.request("textDocument/completion", position("file:///a.fr", 0, 11))), " "); got != "amount fee" {
		t.Errorf("with a declared result the completion is %q", got)
	}
	// Anywhere else in an update the usual names are offered.
	s = newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int"))
	s.open("file:///a.fr", "{...order, amount: ")
	if got := labels(s.request("textDocument/completion", position("file:///a.fr", 0, 19))); !slices.Contains(got, "fee") {
		t.Errorf("a field's value is offered %v", got)
	}
}
