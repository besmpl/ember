package rejected

import "os"

var packageProcess, packageProcessError = os.StartProcess("helper", nil, nil)
