package rejected

import "plugin"

var packagePlugin, packagePluginError = plugin.Open("foreign.so")
