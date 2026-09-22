package std

import (
	"fmt"
	"strings"

	"funroute/lang"
)

// The string functions a routing rule actually writes: normalising a channel
// code, testing a card BIN or a phone prefix, cutting a composite field apart
// and putting reason codes back together. Everything here counts in characters
// (UTF-8 code points), not bytes, because that is what someone reading a rule
// means by "the first six".
func registerStrings(registry *lang.Registry) error {
	for _, fn := range []struct {
		name, label, description, param, result string
		apply                                   func(string) string
	}{
		{"upper", "转大写", "把字符串转成大写。", "文本", "大写文本", strings.ToUpper},
		{"lower", "转小写", "把字符串转成小写。", "文本", "小写文本", strings.ToLower},
		{"trim", "去空白", "去掉字符串两端的空白字符。", "文本", "去掉空白的文本", strings.TrimSpace},
	} {
		doc := lang.Doc{Constexpr: true,
			Label: fn.label, Description: fn.description, Category: "字符串", Cost: 3,
			Params: []string{fn.param}, Result: fn.result,
		}
		apply := fn.apply
		if err := lang.Logic(registry, fn.name, doc, func(text string) (string, error) {
			return apply(text), nil
		}); err != nil {
			return err
		}
	}
	if err := registerStringTests(registry); err != nil {
		return err
	}
	return registerStringParts(registry)
}

func registerStringTests(registry *lang.Registry) error {
	for _, fn := range []struct {
		name, label, description string
		test                     func(string, string) bool
	}{
		{"contains", "包含子串", "文本里有没有出现这个子串。", strings.Contains},
		{"starts_with", "以此开头", "文本是不是以这个前缀开头，卡 BIN 与号段判断用它。", strings.HasPrefix},
		{"ends_with", "以此结尾", "文本是不是以这个后缀结尾。", strings.HasSuffix},
	} {
		doc := lang.Doc{Constexpr: true,
			Label: fn.label, Description: fn.description, Category: "字符串", Cost: 3,
			Params: []string{"文本", "子串"}, Result: "是否命中",
		}
		test := fn.test
		if err := lang.Logic(registry, fn.name, doc, func(text, part string) (bool, error) {
			return test(text, part), nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func registerStringParts(registry *lang.Registry) error {
	if err := lang.Logic(registry, "slice", lang.Doc{Constexpr: true,
		Label:       "取子串",
		Description: "按字符位置取一段，从 start 到 end（不含 end），下标从 0 开始；越界报错，不静默截断。",
		Category:    "字符串", Cost: 4,
		Params: []string{"文本", "起点", "终点"}, Result: "子串",
	}, sliceString); err != nil {
		return err
	}
	if err := lang.Logic(registry, "split", lang.Doc{Constexpr: true,
		Label:       "拆分",
		Description: "按分隔符把文本拆成数组；分隔符为空是错误。",
		Category:    "字符串", Cost: 5,
		Params: []string{"文本", "分隔符"}, Result: "各段",
	}, splitString); err != nil {
		return err
	}
	if err := lang.Logic(registry, "join", lang.Doc{Constexpr: true,
		Label:       "拼接",
		Description: "用分隔符把一组文本连起来，拼原因码用它。",
		Category:    "字符串", Cost: 5,
		Params: []string{"各段", "分隔符"}, Result: "文本",
	}, func(parts []string, separator string) (string, error) {
		return strings.Join(parts, separator), nil
	}); err != nil {
		return err
	}
	return lang.Logic(registry, "replace", lang.Doc{Constexpr: true,
		Label:       "替换",
		Description: "把文本里出现的每一处 old 换成 new。",
		Category:    "字符串", Cost: 5,
		Params: []string{"文本", "被替换", "替换为"}, Result: "替换后的文本",
	}, func(text, old, replacement string) (string, error) {
		return strings.ReplaceAll(text, old, replacement), nil
	})
}

func sliceString(text string, start, end int64) (string, error) {
	runes := []rune(text)
	length := int64(len(runes))
	if start < 0 || end < start || end > length {
		return "", fmt.Errorf("slice [%d, %d) is outside a string of %d characters", start, end, length)
	}
	return string(runes[start:end]), nil
}

func splitString(text, separator string) ([]string, error) {
	if separator == "" {
		return nil, fmt.Errorf("split needs a separator")
	}
	return strings.Split(text, separator), nil
}
