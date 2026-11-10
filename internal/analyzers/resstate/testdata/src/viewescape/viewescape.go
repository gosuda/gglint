package viewescape

import (
	"bufio"
	"bytes"
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

func viewescapeReportedReturn(sc *bufio.Scanner) []byte {
	for sc.Scan() {
		return sc.Bytes() // want "points into the buffer of"
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
		_, _ = r.Discard(1)
		if len(p) == 0 {
			break
		}
	}
	return out
}

func viewescapeReportedBuffer(buf *bytes.Buffer, out [][]byte) [][]byte {
	for buf.Len() > 0 {
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
