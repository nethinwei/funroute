package machine_test

import (
	"encoding/json"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// snapshotRegistry is a small money console whose artifacts, manifest and
// catalog are fixed as snapshots.
func snapshotRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// artifactSnapshot is an artifact with constants, calls, a conversion at
// the quotes of an argument, a record, a local and a money stamp, as JSON: the digest covers all of it.
const artifactSnapshot = `{"version":1,"digest":"sha256:26fbec964b61521a341108a5bd9abe186575450faa156410d2fdd4502ef24a82","expr_json":{"version":1,"expr":{"node":"let","bindings":[{"name":"x","value":{"node":"record","fields":[{"name":"fee","value":{"node":"call","name":"round","args":[{"node":"call","name":"mul","args":[{"node":"var","name":"amount"},{"node":"ratio","value":"2.9","unit":"%"}]},{"node":"enum","member":"half_even"}]}},{"name":"yen","value":{"node":"using","quotes":[{"node":"var","name":"rates"}],"body":{"node":"call","name":"round","args":[{"node":"call","name":"convert","args":[{"node":"var","name":"amount"},{"node":"currency","code":"JPY"}]},{"node":"enum","member":"half_even"}]}}}]}}],"body":{"node":"array","items":[{"node":"field","value":{"node":"var","name":"x"},"field":"fee"},{"node":"money","currency":"USD","amount":"1"}]}}},"args":[{"name":"amount","type":{"kind":"money"},"doc":"金额"},{"name":"rates","type":{"kind":"array","elem":{"kind":"fxrate"}},"doc":"报价"}],"result":{"kind":"array","elem":{"kind":"money"}},"constants":[{"type":"ratio","string":"0.029"},{"type":"string","string":"half_even"},{"type":"currency","string":"JPY"},{"type":"string","string":"half_even"},{"type":"money","int":100,"string":"USD"}],"calls":[{"name":"mul","signature":"mul(money,ratio)-\u003emoney","cost":2},{"name":"round","signature":"round(money,enum\u003crounding\u003e{ceiling,down,floor,half_down,half_even,half_up,up})-\u003emoney","cost":1},{"name":"convert","signature":"convert(money,currency)-\u003emoney","cost":5}],"locals":1,"max_stack":3,"instructions":[{"op":"load_arg"},{"op":"const"},{"op":"call","b":2,"type":{"kind":"money"}},{"op":"const","a":1},{"op":"call","a":1,"b":2,"type":{"kind":"money"}},{"op":"load_arg","a":1},{"op":"fx_push","b":1},{"op":"load_arg"},{"op":"const","a":2},{"op":"call","a":2,"b":2,"type":{"kind":"money"}},{"op":"const","a":3},{"op":"call","a":1,"b":2,"type":{"kind":"money"}},{"op":"fx_pop"},{"op":"make_record","a":2,"type":{"kind":"record","fields":[{"name":"fee","type":{"kind":"money"}},{"name":"yen","type":{"kind":"money"}}]}},{"op":"store_local"},{"op":"load_local"},{"op":"field","type":{"kind":"money"}},{"op":"const","a":4},{"op":"make_array","a":2,"type":{"kind":"array","elem":{"kind":"money"}}}],"money":{"currencies":[{"code":"JPY","digits":0},{"code":"USD","digits":2}]}}`

// An artifact's JSON is what is stored and what its digest covers, so it is
// fixed byte for byte, and it reads back to itself.
func TestArtifactJSONShape(t *testing.T) {
	t.Parallel()
	registry := snapshotRegistry(t)
	amount, _ := machine.ParseType("money")
	artifact, err := compile.CompileExpr(`let(x = {fee: round(amount * 2.9%, @half_even), yen: using(rates, round(amount -> JPY, @half_even))}, [x.fee, USD 1])`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "amount", Type: amount, Doc: "金额"}, {Name: "rates", Type: machine.ArrayOf(machine.FxRateType), Doc: "报价"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(artifact)
	if err != nil || string(encoded) != artifactSnapshot {
		t.Fatalf("json.Marshal(artifact) = %s, %v\nwant %s", encoded, err, artifactSnapshot)
	}
	var back machine.Artifact
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(&back); string(again) != artifactSnapshot {
		t.Fatalf("an artifact read back writes %s", again)
	}
	if _, err := machine.Instantiate(&back, registry); err != nil {
		t.Fatalf("Instantiate(the artifact read back) = %v", err)
	}
}
