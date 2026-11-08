package zipuser

import "archive/zip"

func newWriter() *zip.Writer {
	return zip.NewWriter(nil)
}
