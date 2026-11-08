package checklocks

import (
	"bytes"
	"encoding/gob"
	"maps"
	"slices"
)

type namedGuard struct {
	Name string
	Info functionGuardInfo
}

type namedFieldGuard struct {
	Name     string
	Resolver fieldGuardResolver
}

type lockFunctionFactsWire struct {
	HeldOnEntry     []namedGuard
	HeldOnExit      []namedGuard
	ExcludedOnEntry []namedGuard
	Ignore          bool
}

type lockGuardFactsWire struct {
	GuardedBy         []namedFieldGuard
	ReadAny           bool
	AtomicDisposition atomicDisposition
}

func (lff *lockFunctionFacts) GobEncode() ([]byte, error) {
	return gobBytes(lockFunctionFactsWire{
		HeldOnEntry:     sortedGuards(lff.HeldOnEntry),
		HeldOnExit:      sortedGuards(lff.HeldOnExit),
		ExcludedOnEntry: sortedGuards(lff.ExcludedOnEntry),
		Ignore:          lff.Ignore,
	})
}

func (lff *lockFunctionFacts) GobDecode(data []byte) error {
	var wire lockFunctionFactsWire
	if err := gobInto(data, &wire); err != nil {
		return err
	}
	*lff = lockFunctionFacts{
		HeldOnEntry:     guardMap(wire.HeldOnEntry),
		HeldOnExit:      guardMap(wire.HeldOnExit),
		ExcludedOnEntry: guardMap(wire.ExcludedOnEntry),
		Ignore:          wire.Ignore,
	}
	return nil
}

func (lgf *lockGuardFacts) GobEncode() ([]byte, error) {
	wire := lockGuardFactsWire{ReadAny: lgf.ReadAny, AtomicDisposition: lgf.AtomicDisposition}
	for _, name := range slices.Sorted(maps.Keys(lgf.GuardedBy)) {
		wire.GuardedBy = append(wire.GuardedBy, namedFieldGuard{Name: name, Resolver: lgf.GuardedBy[name]})
	}
	return gobBytes(wire)
}

func (lgf *lockGuardFacts) GobDecode(data []byte) error {
	var wire lockGuardFactsWire
	if err := gobInto(data, &wire); err != nil {
		return err
	}
	*lgf = lockGuardFacts{ReadAny: wire.ReadAny, AtomicDisposition: wire.AtomicDisposition}
	if len(wire.GuardedBy) > 0 {
		lgf.GuardedBy = make(map[string]fieldGuardResolver, len(wire.GuardedBy))
	}
	for _, entry := range wire.GuardedBy {
		lgf.GuardedBy[entry.Name] = entry.Resolver
	}
	return nil
}

func sortedGuards(guards map[string]functionGuardInfo) []namedGuard {
	out := make([]namedGuard, 0, len(guards))
	for _, name := range slices.Sorted(maps.Keys(guards)) {
		out = append(out, namedGuard{Name: name, Info: guards[name]})
	}
	return out
}

func guardMap(entries []namedGuard) map[string]functionGuardInfo {
	if len(entries) == 0 {
		return nil
	}
	guards := make(map[string]functionGuardInfo, len(entries))
	for _, entry := range entries {
		guards[entry.Name] = entry.Info
	}
	return guards
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
