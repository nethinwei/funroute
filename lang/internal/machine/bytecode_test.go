package machine_test

import (
	"encoding/json"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// snapshotRegistry is a small money console whose artifacts, manifest and
// catalog are fixed as snapshots.
func snapshotRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	err := registry.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfUp, Currencies: []machine.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// artifactSnapshot is an artifact with constants, calls, a conversion, a
// record, a local and a money stamp, as JSON: the digest covers all of it.
const artifactSnapshot = `{"version":1,"digest":"sha256:a96c525ccde8e46bcd2a2366d4223f16d406f6f41e726dce555b675bdc450a0c","expr_json":{"version":1,"expr":{"node":"let","bindings":[{"name":"x","value":{"node":"record","fields":[{"name":"fee","value":{"node":"call","name":"mul","args":[{"node":"var","name":"amount"},{"node":"rate","value":"2.9","unit":"%"}]}},{"name":"yen","value":{"node":"call","name":"convert","args":[{"node":"var","name":"amount"},{"node":"currency","code":"JPY"}]}}]}}],"body":{"node":"array","items":[{"node":"field","value":{"node":"var","name":"x"},"field":"fee"},{"node":"money","currency":"USD","amount":"1"}]}}},"args":[{"name":"amount","type":{"kind":"money","name":"USD"},"doc":"金额"}],"result":{"kind":"array","elem":{"kind":"money","name":"USD"}},"constants":[{"type":"rate","int":290000000},{"type":"currency","string":"JPY"},{"type":"money","int":100,"string":"USD"}],"calls":[{"name":"mul","signature":"mul(money\u003cu\u003e,rate)-\u003emoney\u003cu\u003e","cost":2},{"name":"convert","signature":"convert(money\u003ca\u003e,currency\u003cb\u003e)-\u003emoney\u003cb\u003e","cost":5}],"locals":1,"max_stack":3,"instructions":[{"op":"load_arg"},{"op":"const"},{"op":"call","b":2,"type":{"kind":"money","name":"USD"}},{"op":"load_arg"},{"op":"const","a":1},{"op":"call","a":1,"b":2,"type":{"kind":"money","name":"JPY"}},{"op":"make_record","a":2,"type":{"kind":"record","fields":[{"name":"fee","type":{"kind":"money","name":"USD"}},{"name":"yen","type":{"kind":"money","name":"JPY"}}]}},{"op":"store_local"},{"op":"load_local"},{"op":"field","type":{"kind":"money","name":"USD"}},{"op":"const","a":2},{"op":"make_array","a":2,"type":{"kind":"array","elem":{"kind":"money","name":"USD"}}}],"money":{"rounding":"half_up","currencies":[{"code":"JPY","digits":0},{"code":"USD","digits":2}]}}`

// An artifact's JSON is what is stored and what its digest covers, so it is
// fixed byte for byte, and it reads back to itself.
func TestArtifactJSONShape(t *testing.T) {
	t.Parallel()
	registry := snapshotRegistry(t)
	amount, _ := machine.ParseType("money<USD>")
	artifact, err := compile.CompileExpr(`let(x = {fee: amount * 2.9%, yen: amount -> JPY}, [x.fee, USD 1])`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "amount", Type: amount, Doc: "金额"}},
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
