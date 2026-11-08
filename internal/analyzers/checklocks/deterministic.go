package checklocks

import (
	"bytes"
	"encoding/gob"
	"maps"
	"slices"
)

type entry[V any] struct {
	Name  string
	Value V
}

type lockFunctionFactsWire struct {
	HeldOnEntry     []entry[functionGuardInfo]
	HeldOnExit      []entry[functionGuardInfo]
	ExcludedOnEntry []entry[functionGuardInfo]
	Ignore          bool
}

type lockGuardFactsWire struct {
	GuardedBy         []entry[fieldGuardResolver]
	ReadAny           bool
	AtomicDisposition atomicDisposition
}

func (lff *lockFunctionFacts) GobEncode() ([]byte, error) {
	return gobBytes(lockFunctionFactsWire{
		HeldOnEntry:     sortedEntries(lff.HeldOnEntry),
		HeldOnExit:      sortedEntries(lff.HeldOnExit),
		ExcludedOnEntry: sortedEntries(lff.ExcludedOnEntry),
		Ignore:          lff.Ignore,
	})
}

func (lff *lockFunctionFacts) GobDecode(data []byte) error {
	var wire lockFunctionFactsWire
	if err := gobInto(data, &wire); err != nil {
		return err
	}
	*lff = lockFunctionFacts{
		HeldOnEntry:     entryMap(wire.HeldOnEntry),
		HeldOnExit:      entryMap(wire.HeldOnExit),
		ExcludedOnEntry: entryMap(wire.ExcludedOnEntry),
		Ignore:          wire.Ignore,
	}
	return nil
}

func (lgf *lockGuardFacts) GobEncode() ([]byte, error) {
	return gobBytes(lockGuardFactsWire{
		GuardedBy:         sortedEntries(lgf.GuardedBy),
		ReadAny:           lgf.ReadAny,
		AtomicDisposition: lgf.AtomicDisposition,
	})
}

func (lgf *lockGuardFacts) GobDecode(data []byte) error {
	var wire lockGuardFactsWire
	if err := gobInto(data, &wire); err != nil {
		return err
	}
	*lgf = lockGuardFacts{
		GuardedBy:         entryMap(wire.GuardedBy),
		ReadAny:           wire.ReadAny,
		AtomicDisposition: wire.AtomicDisposition,
	}
	return nil
}

func sortedEntries[V any](m map[string]V) []entry[V] {
	out := make([]entry[V], 0, len(m))
	for _, name := range slices.Sorted(maps.Keys(m)) {
		out = append(out, entry[V]{Name: name, Value: m[name]})
	}
	return out
}

func entryMap[V any](entries []entry[V]) map[string]V {
	m := make(map[string]V, len(entries))
	for _, e := range entries {
		m[e.Name] = e.Value
	}
	return m
}

func gobBytes(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gobInto(data []byte, v any) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
}
