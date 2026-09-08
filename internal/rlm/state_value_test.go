package rlm

import (
	"encoding/json"
	"math"
	"math/big"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func TestStateJSONPreservesNumbersAndSharedTrees(t *testing.T) {
	t.Parallel()
	integer, ok := new(big.Int).SetString("9007199254740993123456789", 10)
	if !ok {
		t.Fatal("invalid fixture integer")
	}
	shared := starlark.NewList([]starlark.Value{starlark.MakeBigInt(integer), starlark.Float(2), starlark.Float(2.5), starlark.None, starlark.True})
	dict := starlark.NewDict(2)
	for _, key := range []string{"first", "second"} {
		if err := dict.SetKey(starlark.String(key), shared); err != nil {
			t.Fatal(err)
		}
	}
	value, err := stateToGo(dict)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"first":[9007199254740993123456789,2.0,2.5,null,true],"second":[9007199254740993123456789,2.0,2.5,null,true]}`
	if string(encoded) != want {
		t.Fatalf("state serialization lost JSON types or precision: %s", encoded)
	}
}

func TestStateJSONRejectsLossyAndCyclicValues(t *testing.T) {
	t.Parallel()
	listCycle := starlark.NewList(nil)
	if err := listCycle.Append(listCycle); err != nil {
		t.Fatal(err)
	}
	dictCycle := starlark.NewDict(1)
	if err := dictCycle.SetKey(starlark.String("cycle"), dictCycle); err != nil {
		t.Fatal(err)
	}
	badKey, invalidKey, badChild := starlark.NewDict(1), starlark.NewDict(1), starlark.NewDict(1)
	for _, entry := range []struct {
		dict       *starlark.Dict
		key, value starlark.Value
	}{
		{badKey, starlark.MakeInt(1), starlark.None},
		{invalidKey, starlark.String("\xff"), starlark.None},
		{badChild, starlark.String("child"), starlark.Bytes("data")},
	} {
		if err := entry.dict.SetKey(entry.key, entry.value); err != nil {
			t.Fatal(err)
		}
	}
	var deep starlark.Value = starlark.String("leaf")
	for range 102 {
		deep = starlark.NewList([]starlark.Value{deep})
	}
	for _, test := range []struct {
		name      string
		value     starlark.Value
		errorText string
	}{
		{"list cycle", listCycle, "cycles"},
		{"dict cycle", dictCycle, "cycles"},
		{"numeric key", badKey, "string keys"},
		{"invalid key", invalidKey, "UTF-8"},
		{"invalid string", starlark.String("\xff"), "UTF-8"},
		{"bytes", starlark.Bytes("data"), "does not support bytes"},
		{"tuple", starlark.Tuple{starlark.None}, "does not support tuple"},
		{"dict child", badChild, "does not support bytes"},
		{"list child", starlark.NewList([]starlark.Value{starlark.Bytes("data")}), "does not support bytes"},
		{"depth", deep, "maximum depth"},
		{"infinity", starlark.Float(math.Inf(1)), "finite"},
		{"nan", starlark.Float(math.NaN()), "finite"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := stateToGo(test.value); err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("invalid state accepted: %v", err)
			}
		})
	}
}
