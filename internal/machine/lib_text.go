package machine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// The string functions a routing rule writes most: normalising a channel
// code, testing a card BIN or a phone prefix, cutting a composite field apart
// and putting reason codes back together. Everything here counts in
// characters (UTF-8 code points), not bytes, because that is what someone
// reading a rule means by "the first six".
func caseSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 3)
	for _, fn := range []struct {
		name, label, description, param, result string
		apply                                   func(string) string
	}{
		{"upper", "转大写", "把字符串转成大写。", "文本", "大写文本", keepingBytes(strings.ToUpper, unicode.ToUpper)},
		{"lower", "转小写", "把字符串转成小写。", "文本", "小写文本", keepingBytes(strings.ToLower, unicode.ToLower)},
		{"trim", "去空白", "去掉字符串两端的空白字符。", "文本", "去掉空白的文本", strings.TrimSpace},
	} {
		doc := Doc{
			Label: fn.label, Description: fn.description, Category: "字符串",
			Params: []string{fn.param}, Result: fn.result,
		}
		apply := fn.apply
		specs = append(specs, libGo(fn.name, doc, func(text string) (string, error) {
			return apply(text), nil
		}))
	}
	return specs
}

// keepingBytes is whole, a case mapping, for text that is UTF-8, and for
// text that is not, each character mapped by to with every byte that is no
// character kept as it is: strings.ToUpper would write U+FFFD for them, and
// changing a text's case should not change its other bytes.
func keepingBytes(whole func(string) string, to func(rune) rune) func(string) string {
	return func(text string) string {
		if utf8.ValidString(text) {
			return whole(text)
		}
		var out strings.Builder
		out.Grow(len(text))
		for i, r := range text {
			if _, size := utf8.DecodeRuneInString(text[i:]); r == utf8.RuneError && size == 1 {
				out.WriteByte(text[i])
				continue
			}
			out.WriteRune(to(r))
		}
		return out.String()
	}
}

func testSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 3)
	for _, fn := range []struct {
		name, label, description string
		test                     func(string, string) bool
	}{
		{"contains", "包含子串", "文本里有没有出现这个子串。", strings.Contains},
		{"starts_with", "以此开头", "文本是不是以这个前缀开头，卡 BIN 与号段判断用它。", strings.HasPrefix},
		{"ends_with", "以此结尾", "文本是不是以这个后缀结尾。", strings.HasSuffix},
	} {
		doc := Doc{
			Label: fn.label, Description: fn.description, Category: "字符串",
			Params: []string{"文本", "子串"}, Result: "是否命中",
		}
		specs = append(specs, libGo(fn.name, doc, fn.test))
	}
	return specs
}

func partSpecs() []FunctionSpec {
	return []FunctionSpec{
		libGo("slice", Doc{
			Label:       "取子串",
			Description: "按字符位置取一段，从 start 到 end（不含 end），下标从 0 开始；越界报错，不静默截断。",
			Category:    "字符串",
			Params:      []string{"文本", "起点", "终点"}, Result: "子串",
		}, sliceString),
		libGo("split", Doc{
			Label:       "拆分",
			Description: "按分隔符把文本拆成数组；分隔符为空是错误。",
			Category:    "字符串",
			Params:      []string{"文本", "分隔符"}, Result: "各段",
		}, splitString),
		libGo("join", Doc{
			Label:       "拼接",
			Description: "用分隔符把一组文本连起来，拼原因码用它。",
			Category:    "字符串",
			Params:      []string{"各段", "分隔符"}, Result: "文本",
		}, func(parts []string, separator string) (string, error) {
			return strings.Join(parts, separator), nil
		}),
		libGo("replace", Doc{
			Label:       "替换",
			Description: "把文本里出现的每一处 old 换成 new。",
			Category:    "字符串",
			Params:      []string{"文本", "被替换", "替换为"}, Result: "替换后的文本",
		}, func(text, old, replacement string) (string, error) {
			return strings.ReplaceAll(text, old, replacement), nil
		}),
	}
}

// sliceString cuts the text itself at the characters' bytes, so a byte that
// is not UTF-8 is kept as it is, and nothing is copied.
func sliceString(text string, start, end int64) (string, error) {
	length := int64(utf8.RuneCountInString(text))
	if start < 0 || end < start || end > length {
		return "", fmt.Errorf("%w: slice [%d, %d) is outside a string of %d characters", ErrDomain, start, end, length)
	}
	return text[byteOf(text, start):byteOf(text, end)], nil
}

// byteOf is where character n of text starts, len(text) past its last.
func byteOf(text string, n int64) int {
	for i := range text {
		if n == 0 {
			return i
		}
		n--
	}
	return len(text)
}

func splitString(text, separator string) ([]string, error) {
	if separator == "" {
		return nil, fmt.Errorf("%w: split needs a separator", ErrDomain)
	}
	return strings.Split(text, separator), nil
}

// searchSpecs find a part of a text and cut a known one off it: where a
// separator sits, a channel prefix taken off a code, and a test against a
// pattern. Positions count characters, as slice's do.
func searchSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 5)
	for _, fn := range []struct {
		name, label, description string
		find                     func(string, string) int
	}{
		{"index_of", "子串位置", "子串第一次出现的字符位置，从 0 开始；不在里面是错误，先用 contains 判断。", strings.Index},
		{"last_index_of", "子串最后的位置", "子串最后一次出现的字符位置；不在里面是错误。", strings.LastIndex},
	} {
		find := fn.find
		specs = append(specs, FunctionSpec{
			Name: fn.name, Params: []Type{StringType, StringType}, Result: IntType,
			Eval: func(_ context.Context, args []Value) (Value, error) {
				text, part := args[0].s, args[1].s
				at := find(text, part)
				if at < 0 {
					return Value{}, fmt.Errorf("%w: %q is not in the text", ErrDomain, part)
				}
				return Int(int64(utf8.RuneCountInString(text[:at]))), nil
			},
			Doc: Doc{Label: fn.label, Description: fn.description, Category: "字符串", Params: []string{"文本", "子串"}, Result: "位置"},
		})
	}
	for _, fn := range []struct {
		name, label, description string
		trim                     func(string, string) string
	}{
		{"trim_prefix", "去掉前缀", "文本以这个前缀开头就去掉它，否则原样返回。", strings.TrimPrefix},
		{"trim_suffix", "去掉后缀", "文本以这个后缀结尾就去掉它，否则原样返回。", strings.TrimSuffix},
	} {
		trim := fn.trim
		specs = append(specs, libGo(fn.name, Doc{
			Label: fn.label, Description: fn.description, Category: "字符串", Params: []string{"文本", "前缀或后缀"}, Result: "文本",
		}, func(text, part string) (string, error) { return trim(text, part), nil }))
	}
	return append(specs, FunctionSpec{
		Name: "matches", Params: []Type{StringType, StringType}, Result: BoolType, Eval: matchText, checkConstant: checkPattern,
		Doc: Doc{
			Label: "正则匹配", Category: "字符串", ConstArgs: []int{1},
			Description: `文本里有没有与模式匹配的一段；要整段匹配就写 ^…$。模式是 RE2 语法，匹配时间与文本长度成正比；模式必须在编译时定下（字面量或折叠出的常量），写错是编译错误。文本至多 10000 字节。`,
			Params:      []string{"文本", "模式"}, Result: "是否匹配",
		},
	})
}

// maxMatchedText caps the text matches reads. RE2 is linear in it, and what
// a rule tests is a field — a card number, a reference — not a document.
const maxMatchedText = 10_000

func matchText(_ context.Context, args []Value) (Value, error) {
	text := args[0].s
	if len(text) > maxMatchedText {
		return Value{}, fmt.Errorf("%w: matches reads at most %d bytes, the text has %d", ErrDomain, maxMatchedText, len(text))
	}
	compiled, err := pattern(args[1].s)
	if err != nil {
		return Value{}, fmt.Errorf("%w: %w", ErrDomain, err)
	}
	return Bool(compiled.MatchString(text)), nil
}

func checkPattern(value Value) error {
	_, err := pattern(value.s)
	return err
}

// patterns is every pattern compiled so far, by its text. A pattern is a
// constant of a compiled rule, so there are as many as the rules write; the
// count stops the cache growing past what any host's rules do, and a pattern
// past it is compiled again on each call.
var (
	patterns      sync.Map
	patternsCount atomic.Int32
)

const maxPatterns = 4096

func pattern(text string) (*regexp.Regexp, error) {
	if cached, ok := patterns.Load(text); ok {
		compiled, _ := cached.(*regexp.Regexp)
		return compiled, nil
	}
	compiled, err := regexp.Compile(text)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %s", text, strings.TrimPrefix(err.Error(), "error parsing regexp: "))
	}
	if patternsCount.Add(1) <= maxPatterns {
		patterns.Store(text, compiled)
	}
	return compiled, nil
}
