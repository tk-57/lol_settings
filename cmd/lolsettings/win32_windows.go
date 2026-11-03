package main

// Win32 constants used by the window procedure and confirmation dialog.
const (
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmClose          = 0x0010
	wmSetCursor      = 0x0020
	wmDrawItem       = 0x002B
	wmCommand        = 0x0111
	wmWorkComplete   = 0x8001 // WM_APP + 1
	mbYesNo          = 0x0004
	mbIconQuestion   = 0x0020
	mbDefaultButton2 = 0x0100
	idYes            = 6
)
