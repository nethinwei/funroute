package std

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nethinwei/funroute"
)

// paddingSpecs are for the places a payment file or an order number has a
// fixed width: 00001234, a 20-character reconciliation column.
func paddingSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 2)
	for _, side := range []struct {
		name, label, side string
		left              bool
	}{
		{"pad_left", "左侧补齐", "左侧", true},
		{"pad_right", "右侧补齐", "右侧", false},
	} {
		doc := funroute.Doc{
			Label: side.label, Category: "字符串",
			Description: "把文本补到指定的字符数（至多 10000），在" + side.side + "补；填充串必须是一个字符。已经够长就原样返回 —— 截断会悄悄丢掉数据。",
			Params:      []string{"文本", "宽度", "填充"}, Result: "补齐后的文本",
		}
		left := side.left
		specs = append(specs, logic(side.name, doc, func(text string, width int64, fill string) (string, error) {
			return padTo(text, width, fill, left)
		}))
	}
	return specs
}

// maxPadWidth caps the width a padding call builds. A fixed-width field in a
// payment file is tens of characters; the cap is what keeps a width written
// as 4000000000 from allocating gigabytes — in a run, and in the compiler,
// which folds a constant call.
const maxPadWidth = 10_000

func padTo(text string, width int64, fill string, left bool) (string, error) {
	if utf8.RuneCountInString(fill) != 1 {
		return "", fmt.Errorf("%w: the padding must be exactly one character, got %q", funroute.ErrDomain, fill)
	}
	if width < 0 || width > maxPadWidth {
		return "", fmt.Errorf("%w: a width is 0 to %d characters, got %d", funroute.ErrArithmetic, maxPadWidth, width)
	}
	missing := int(width) - utf8.RuneCountInString(text)
	if missing <= 0 {
		return text, nil
	}
	padding := strings.Repeat(fill, missing)
	if left {
		return padding + text, nil
	}
	return text + padding, nil
}
