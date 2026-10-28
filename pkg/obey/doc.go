// Package obey provides a typed boundary to the installed Obey CLI.
//
// The CLI owns daemon authentication and transport. Construction does not
// probe a daemon; operations require an installed, compatible Obey executable.
package obey
