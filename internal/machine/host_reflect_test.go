package machine

import (
	"reflect"
	"strings"
	"testing"
)

type selfNode struct {
	Children []selfNode `funroute:"children"`
}

type mutualA struct {
	B []mutualB `funroute:"b"`
}

type mutualB struct {
	As map[string]mutualA `funroute:"as"`
}

type selfSlice []selfSlice

type siblings struct {
	Left  narrowOrder   `funroute:"left"`
	Right narrowOrder   `funroute:"right"`
	All   []narrowOrder `funroute:"all"`
}

// A Go type that holds itself, directly or through another, would be derived
// forever: it is refused with an error instead of overflowing the stack. Only
// the path counts, so the same type twice side by side is fine.
func TestReflectTypeRefusesARecursiveType(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeFor[selfNode](),
		reflect.TypeFor[[]selfNode](),
		reflect.TypeFor[mutualB](),
		reflect.TypeFor[selfSlice](),
	} {
		t.Run(typ.String(), func(t *testing.T) {
			t.Parallel()
			got, err := reflectType(nil, typ)
			if err == nil || !strings.Contains(err.Error(), "it contains itself") {
				t.Fatalf("reflectType(%s) = %v, %v, want it refused as recursive", typ, got.Summary(), err)
			}
		})
	}
	got, err := reflectType(nil, reflect.TypeFor[siblings]())
	want := RecordOf(FieldOf("left", RecordOf(FieldOf("amount", IntType))), FieldOf("right", RecordOf(FieldOf("amount", IntType))),
		FieldOf("all", ArrayOf(RecordOf(FieldOf("amount", IntType)))))
	if err != nil || !got.Equal(want) {
		t.Fatalf("reflectType(siblings) = %v, %v, want %s", got.Summary(), err, want.Summary())
	}
}
