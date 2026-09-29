package capability

import "errors"

var ErrStaleAdmission = errors.New("capability admission changed")
