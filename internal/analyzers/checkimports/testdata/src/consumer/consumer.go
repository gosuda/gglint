package consumer

import (
	_ "archive/tar" // want `import of "archive/tar" is denied in consumer`
	_ "archive/zip"
	_ "crypto/sha256"
	_ "encoding/json" // want `import of "encoding/json" is denied in consumer`
	_ "net"           // want `import of "net" is denied in consumer`
	_ "net/http"      // want `import of "net/http" is denied in consumer`
	_ "network"
)
