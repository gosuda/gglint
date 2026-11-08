// Copyright 2022 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package atomicbitops is a stdlib-only subset of gvisor.dev/gvisor/pkg/atomicbitops
// holding the types and methods the checklocks fixtures call.
//
// +checkalignedignore
package atomicbitops

import "sync/atomic"

// Int32 is an atomic int32.
//
// The default value is zero.
type Int32 struct {
	value int32
}

// FromInt32 returns an Int32 initialized to value v.
func FromInt32(v int32) Int32 {
	return Int32{value: v}
}

// Load is analogous to atomic.LoadInt32.
func (i *Int32) Load() int32 {
	return atomic.LoadInt32(&i.value)
}

// RacyLoad is analogous to reading an atomic value without using
// synchronization.
func (i *Int32) RacyLoad() int32 {
	return i.value
}

// Store is analogous to atomic.StoreInt32.
func (i *Int32) Store(v int32) {
	atomic.StoreInt32(&i.value, v)
}

// RacyStore is analogous to setting an atomic value without using
// synchronization.
func (i *Int32) RacyStore(v int32) {
	i.value = v
}

// Add is analogous to atomic.AddInt32.
func (i *Int32) Add(v int32) int32 {
	return atomic.AddInt32(&i.value, v)
}

// RacyAdd is analogous to adding to an atomic value without using
// synchronization.
func (i *Int32) RacyAdd(v int32) int32 {
	i.value += v
	return i.value
}

// Swap is analogous to atomic.SwapInt32.
func (i *Int32) Swap(v int32) int32 {
	return atomic.SwapInt32(&i.value, v)
}

// Uint64 is an atomic uint64.
type Uint64 struct {
	value uint64
}

// Load is analogous to atomic.LoadUint64.
func (u *Uint64) Load() uint64 {
	return atomic.LoadUint64(&u.value)
}

// RacyLoad is analogous to reading an atomic value without using
// synchronization.
func (u *Uint64) RacyLoad() uint64 {
	return u.value
}

// Store is analogous to atomic.StoreUint64.
func (u *Uint64) Store(v uint64) {
	atomic.StoreUint64(&u.value, v)
}
