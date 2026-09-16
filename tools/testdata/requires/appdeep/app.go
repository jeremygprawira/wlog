// Package appdeep imports a package of a sub-module on purpose. The parent
// module of that package stays indirect, because the code never imports it.
package appdeep

import "example.com/subpkg/sub"
