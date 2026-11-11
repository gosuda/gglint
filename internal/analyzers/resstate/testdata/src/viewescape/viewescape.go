package viewescape

import (
	"bufio"
	"bytes"
	"sync"
)

func viewescapeReported(sc *bufio.Scanner, lines [][]byte) [][]byte {
	for sc.Scan() {
		lines = append(lines, sc.Bytes()) // want "points into the buffer of"
	}
	return lines
}

func viewescapeReportedSend(sc *bufio.Scanner, ch chan []byte) {
	for sc.Scan() {
		ch <- sc.Bytes() // want "points into the buffer of"
	}
}

type viewHolder struct {
	b []byte
}

func viewescapeReportedField(sc *bufio.Scanner, h *viewHolder) {
	for sc.Scan() {
		h.b = sc.Bytes() // want "points into the buffer of"
	}
}

func viewescapeReportedMap(sc *bufio.Scanner, m map[int][]byte, k int) {
	for sc.Scan() {
		m[k] = sc.Bytes() // want "points into the buffer of"
	}
}

func viewescapeReportedOutside(sc *bufio.Scanner) []byte {
	var out []byte
	for sc.Scan() {
		out = sc.Bytes() // want "points into the buffer of"
	}
	return out
}

func viewescapeSilentReturn(sc *bufio.Scanner) []byte {
	for sc.Scan() {
		return sc.Bytes()
	}
	return nil
}

func viewescapeReportedVar(sc *bufio.Scanner, lines [][]byte) [][]byte {
	for sc.Scan() {
		v := sc.Bytes()
		lines = append(lines, v) // want "points into the buffer of"
	}
	return lines
}

func viewescapeReportedPeek(r *bufio.Reader, out [][]byte) [][]byte {
	for {
		p, _ := r.Peek(1)
		out = append(out, p) // want "points into the buffer of"
		_, _ = r.Discard(2)
		if len(p) == 0 {
			break
		}
	}
	return out
}

func viewescapeReportedBuffer(buf *bytes.Buffer, out [][]byte) [][]byte {
	for i := 0; i < 2; i++ {
		out = append(out, buf.Bytes()) // want "points into the buffer of"
		_, _ = buf.Write([]byte("x"))
	}
	return out
}

func viewescapeSilent(sc *bufio.Scanner, lines [][]byte, names []string) ([]string, [][]byte) {
	for sc.Scan() {
		lines = append(lines, bytes.Clone(sc.Bytes()))
	}
	for sc.Scan() {
		names = append(names, string(sc.Bytes()))
	}
	return names, lines
}

func viewescapeSilentSpread(sc *bufio.Scanner, buf []byte) []byte {
	for sc.Scan() {
		buf = append(buf, sc.Bytes()...)
	}
	return buf
}

func viewescapeSilentLoopLocal(sc *bufio.Scanner) int {
	n := 0
	for sc.Scan() {
		var b []byte
		b = sc.Bytes()
		n += len(b)
	}
	return n
}

func viewescapeSilentLoopArray(sc *bufio.Scanner) int {
	n := 0
	for sc.Scan() {
		var row [1][]byte
		row[0] = sc.Bytes()
		n += len(row[0])
	}
	return n
}

func viewescapeSilentReassigned(sc *bufio.Scanner) [][]byte {
	var keep [][]byte
	for sc.Scan() {
		b := sc.Bytes()
		b = bytes.Clone(b)
		keep = append(keep, b)
	}
	return keep
}

func viewescapeReportedNested(sc *bufio.Scanner) [][]byte {
	var lines [][]byte
	for sc.Scan() {
		for sc.Scan() {
			lines = append(lines, sc.Bytes()) // want "points into the buffer of"
		}
	}
	return lines
}

func viewescapeReportedPostWrite(buf *bytes.Buffer, out [][]byte) [][]byte {
	for i := 0; i < 2; _, _ = buf.Write([]byte("x")) {
		out = append(out, buf.Bytes()) // want "points into the buffer of"
		i++
	}
	return out
}

func viewescapeReportedPreLoop(sc *bufio.Scanner) [][]byte {
	var out [][]byte
	if !sc.Scan() {
		return nil
	}
	line := sc.Bytes()
	for sc.Scan() {
		out = append(out, line) // want "points into the buffer of"
	}
	return out
}

func viewescapeReportedMultiTarget(r *bufio.Reader, h *viewHolder) {
	for range 2 {
		h.b, _ = r.Peek(1) // want "points into the buffer of"
		_, _ = r.Discard(2)
	}
}

func viewescapeSilentBufferRead(buf *bytes.Buffer, out [][]byte) [][]byte {
	for buf.Len() > 0 {
		out = append(out, buf.Bytes())
		_, _ = buf.Read(make([]byte, 1))
	}
	return out
}


func viewescapeSilentSelfInvalid(b *bufio.Reader) []byte {
	var frag []byte
	for {
		var e error
		frag, e = b.ReadSlice('\n')
		if e == nil {
			break
		}
		if e != bufio.ErrBufferFull {
			break
		}
	}
	return frag
}

type viewConn struct{ *bufio.Scanner }

func (viewConn) Write(p []byte) (int, error) { return len(p), nil }

func viewescapeSilentForeignInvalidator(c viewConn, out [][]byte) [][]byte {
	for _, s := range []string{"a"} {
		_ = s
		out = append(out, c.Bytes())
		_, _ = c.Write(nil)
	}
	return out
}

func viewescapeReportedRangeChannel(sc *bufio.Scanner, ch chan []byte, out [][]byte) [][]byte {
	for sc.Scan() {
		b := sc.Bytes()
		for b = range ch {
		}
		out = append(out, b) // want "points into the buffer of"
	}
	return out
}

func viewescapeReportedRangeSlice(sc *bufio.Scanner, xs [][]byte, out [][]byte) [][]byte {
	for sc.Scan() {
		b := sc.Bytes()
		for _, b = range xs {
		}
		out = append(out, b) // want "points into the buffer of"
	}
	return out
}

func viewescapeSilentDiscardedPeek(r *bufio.Reader) {
	var err error
	for {
		_, err = r.Peek(1)
		if err != nil {
			break
		}
		_, _ = r.Discard(1)
	}
}

func viewescapeSilentLoopLocalField(sc *bufio.Scanner) int {
	n := 0
	for sc.Scan() {
		var rec viewHolder
		rec.b = sc.Bytes()
		n += len(rec.b)
	}
	return n
}

func viewescapeSilentFreshBuffer(in []string, out [][]byte) [][]byte {
	for _, s := range in {
		var b bytes.Buffer
		_, _ = b.WriteString(s)
		out = append(out, b.Bytes())
	}
	return out
}

func viewescapeSilentLocalReturn(s string) []byte {
	sc := bufio.NewScanner(bytes.NewBufferString(s))
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			return sc.Bytes()
		}
	}
	return nil
}

func viewescapeSilentAssignBreak(s string) []byte {
	sc := bufio.NewScanner(bytes.NewBufferString(s))
	var first []byte
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			first = sc.Bytes()
			break
		}
	}
	return first
}

func viewescapeReportedAssignBreakKept(s string) []byte {
	sc := bufio.NewScanner(bytes.NewBufferString(s))
	var first []byte
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			first = sc.Bytes() // want "points into the buffer of"
			break
		}
	}
	sc.Scan()
	return first
}

func viewescapeReportedLoopLocalFieldEscape(sc *bufio.Scanner) []viewHolder {
	var all []viewHolder
	for sc.Scan() {
		var h viewHolder
		h.b = sc.Bytes() // want "points into the buffer of"
		all = append(all, h)
	}
	return all
}

func viewescapeReportedLoopLocalMapEscape(sc *bufio.Scanner) []map[int][]byte {
	var all []map[int][]byte
	for sc.Scan() {
		m := map[int][]byte{}
		m[0] = sc.Bytes() // want "points into the buffer of"
		all = append(all, m)
	}
	return all
}

func viewescapeSilentAssignBreakParam(sc *bufio.Scanner) []byte {
	var out []byte
	for sc.Scan() {
		out = sc.Bytes()
		break
	}
	return out
}

func viewescapeSilentAssignReturnParam(sc *bufio.Scanner) []byte {
	var out []byte
	for sc.Scan() {
		out = sc.Bytes()
		return out
	}
	return nil
}

func viewescapeSilentPeekReassigned(r *bufio.Reader) (n int, err error) {
	var line []byte
	for {
		line, err = r.Peek(2)
		if err != nil || string(line) == "$$" {
			return
		}
		line, err = r.ReadSlice('\n')
		n += len(line)
	}
}


func viewescapeSilentAssignBreakClone(sc *bufio.Scanner) []byte {
	var line []byte
	for sc.Scan() {
		line = sc.Bytes()
		break
	}
	return bytes.Clone(line)
}

func viewescapeSilentAttrCopy(line []byte) []byte {
	var buf bytes.Buffer
	for i := range line {
		if line[i] == '"' {
			return buf.Bytes()
		}
		buf.Write([]byte{line[i]})
	}
	return nil
}

func viewescapeSilentHeadersCopy(r *bufio.Reader) (out []string) {
	var line []byte
	for {
		line, _ = r.Peek(2)
		if string(line) == "$$" {
			return out
		}
		var err error
		line, err = r.ReadSlice('\n')
		if err != nil && err != bufio.ErrBufferFull {
			return out
		}
		out = append(out, string(line))
	}
}


func viewescapeReportedDeferredPut(sc *bufio.Scanner, pool *sync.Pool) []byte {
	var buf bytes.Buffer
	for sc.Scan() {
		buf.Write(sc.Bytes())
		defer pool.Put(buf)
		return buf.Bytes() // want "points into the buffer of"
	}
	return nil
}

func viewescapeReportedVarAssign(sc *bufio.Scanner) [][]byte {
	var lines [][]byte
	var line []byte
	for sc.Scan() {
		line = sc.Bytes()
		lines = append(lines, line) // want "points into the buffer of"
	}
	return lines
}

func viewescapeReportedEnclosing(sc *bufio.Scanner) []struct{ b []byte } {
	var all []struct{ b []byte }
	for sc.Scan() {
		var rec struct {
			c struct{ b []byte }
		}
		rec.c.b = sc.Bytes() // want "points into the buffer of"
		all = append(all, rec.c)
	}
	return all
}

func viewescapeReportedAssignUse(sc *bufio.Scanner) {
	var b []byte
	for sc.Scan() {
		if len(b) > 0 {
			_ = b[0]
		}
		b = sc.Bytes() // want "points into the buffer of"
	}
}

func viewescapeReportedAssignBareReturn(sc *bufio.Scanner) (out []byte) {
	for sc.Scan() {
		out = sc.Bytes() // want "points into the buffer of"
	}
	return
}

func viewescapeSilentFieldAssignBreak(s string, h *viewHolder) {
	sc := bufio.NewScanner(bytes.NewBufferString(s))
	for sc.Scan() {
		h.b = sc.Bytes()
		break
	}
}
func viewescapeSilentAssignDiscard(sc *bufio.Scanner) {
	var b []byte
	for sc.Scan() {
		_ = b
		b = sc.Bytes()
	}
}
func viewescapeSilentRangeReassignSlice(sc *bufio.Scanner, xs [][]byte, out [][]byte) [][]byte {
	for sc.Scan() {
		b := sc.Bytes()
		for _, b = range xs {
			out = append(out, b)
		}
	}
	return out
}

func viewescapeSilentRangeReassignChannel(sc *bufio.Scanner, ch chan []byte, out [][]byte) [][]byte {
	for sc.Scan() {
		b := sc.Bytes()
		for b = range ch {
			out = append(out, b)
		}
	}
	return out
}

func viewescapeReportedRangeSplitView(sc *bufio.Scanner, sep []byte, lines [][]byte) [][]byte {
	var b []byte
	for sc.Scan() {
		b = sc.Bytes()
		for _, b = range bytes.Split(b, sep) {
			lines = append(lines, b) // want "points into the buffer of"
		}
	}
	return lines
}

func viewescapeReportedOuterRangeInnerView(chunks [][]byte, sc *bufio.Scanner, lines [][]byte) [][]byte {
	var b []byte
	for _, b = range chunks {
		for sc.Scan() {
			b = sc.Bytes()
			lines = append(lines, b) // want "points into the buffer of"
		}
	}
	return lines
}
func cloneAll(v []byte) [][]byte { return [][]byte{bytes.Clone(v)} }

func viewescapeSilentRangeCloneCall(sc *bufio.Scanner, out [][]byte) [][]byte {
	var b []byte
	for sc.Scan() {
		b = sc.Bytes()
		for _, b = range cloneAll(b) {
			out = append(out, b)
		}
	}
	return out
}

func viewescapeReportedRangeDirectViewCall(sc *bufio.Scanner, sep []byte, lines [][]byte) [][]byte {
	var b []byte
	for sc.Scan() {
		b = sc.Bytes()
		for _, b = range bytes.Split(sc.Bytes(), sep) {
			lines = append(lines, b) // want "points into the buffer of"
		}
	}
	return lines
}

func viewescapeReportedRangeAppendAlias(sc *bufio.Scanner, rows [][]byte, out [][]byte) [][]byte {
	var b []byte
	for sc.Scan() {
		b = sc.Bytes()
		for _, b = range append(rows, b) { // want "points into the buffer of"
			out = append(out, b) // want "points into the buffer of"
		}
	}
	return out
}

func viewescapeReportedRangeSplitSeq(sc *bufio.Scanner, sep []byte, out [][]byte) [][]byte {
	var b []byte
	for sc.Scan() {
		b = sc.Bytes()
		for b = range bytes.SplitSeq(b, sep) {
			out = append(out, b) // want "points into the buffer of"
		}
	}
	return out
}

func viewescapeSilentRangeSplitSeqSeparator(sc *bufio.Scanner, data []byte, out [][]byte) [][]byte {
	var b []byte
	for sc.Scan() {
		b = sc.Bytes()
		for b = range bytes.SplitSeq(data, b) {
			out = append(out, b)
		}
	}
	return out
}

func viewescapeReportedSharedAddr(in []string, out [][]byte) [][]byte {
	var shared bytes.Buffer
	for _, s := range in {
		b := &shared
		_, _ = b.WriteString(s)
		out = append(out, b.Bytes()) // want "points into the buffer of"
	}
	return out
}

func viewescapeReportedFreshReset(in []string, out [][]byte) [][]byte {
	for _, s := range in {
		var b bytes.Buffer
		_, _ = b.WriteString(s)
		out = append(out, b.Bytes()) // want "points into the buffer of"
		b.Reset()
		_, _ = b.WriteString("x")
	}
	return out
}
