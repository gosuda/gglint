// want package:"package layout"
package layout

const PageSize = 4096

type Header struct {
	Magic uint32
	Flags uint16
	Kind  uint16
}
