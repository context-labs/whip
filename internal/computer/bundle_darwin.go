//go:build darwin

package computer

import _ "embed"

// Distribution builds replace the empty placeholder with the signed helper.
// Publication uses these exact bytes; it never searches the development tree.
//
//go:embed bin/whip-computer
var helperBinary []byte
