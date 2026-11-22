package main

import "github.com/vivaui/viva/gfx"

var pinIcon = gfx.Icon{
	ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(8, 3), gfx.LineTo(16, 3),
		gfx.MoveTo(10, 3), gfx.LineTo(10, 9), gfx.LineTo(6, 14), gfx.LineTo(18, 14), gfx.LineTo(14, 9), gfx.LineTo(14, 3),
		gfx.MoveTo(12, 14), gfx.LineTo(12, 21),
	},
}

var tiltedPinIcon = gfx.Icon{
	ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(14.5, 3.5), gfx.LineTo(20.5, 9.5),
		gfx.MoveTo(16, 5), gfx.LineTo(12, 9), gfx.LineTo(6.5, 10.5), gfx.LineTo(13.5, 17.5), gfx.LineTo(15, 12), gfx.LineTo(19, 8),
		gfx.MoveTo(10, 14), gfx.LineTo(5, 19),
	},
}

func pinStateIcon(pinned bool) gfx.Icon {
	if pinned {
		return pinIcon
	}
	return tiltedPinIcon
}

var closeIcon = gfx.Icon{
	ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(7, 7), gfx.LineTo(17, 17), gfx.MoveTo(17, 7), gfx.LineTo(7, 17)},
}

var chevronDownIcon = gfx.Icon{
	ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(7, 10), gfx.LineTo(12, 15), gfx.LineTo(17, 10)},
}
var chevronLeftIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(15, 6), gfx.LineTo(9, 12), gfx.LineTo(15, 18),
}}

var chevronRightIcon = gfx.Icon{
	ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(10, 7), gfx.LineTo(15, 12), gfx.LineTo(10, 17)},
}

var menuIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(5, 6), gfx.LineTo(19, 6), gfx.MoveTo(5, 12), gfx.LineTo(19, 12), gfx.MoveTo(5, 18), gfx.LineTo(19, 18)},
}
var leftPanelIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 20), gfx.LineTo(4, 20), gfx.LineTo(4, 4), gfx.MoveTo(10, 4), gfx.LineTo(10, 20), gfx.MoveTo(7, 8), gfx.LineTo(7, 16)},
}
var rightPanelIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 20), gfx.LineTo(4, 20), gfx.LineTo(4, 4), gfx.MoveTo(14, 4), gfx.LineTo(14, 20), gfx.MoveTo(17, 8), gfx.LineTo(17, 16)},
}
var folderIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(3, 6), gfx.LineTo(10, 6), gfx.LineTo(12, 8), gfx.LineTo(21, 8), gfx.LineTo(21, 19), gfx.LineTo(3, 19), gfx.LineTo(3, 6)},
}
var folderOpenIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(3, 19), gfx.LineTo(3, 6), gfx.LineTo(10, 6), gfx.LineTo(12, 8), gfx.LineTo(20, 8), gfx.LineTo(20, 11),
		gfx.MoveTo(3, 19), gfx.LineTo(7, 11), gfx.LineTo(22, 11), gfx.LineTo(18, 19), gfx.LineTo(3, 19),
	},
}
var fileIcon = gfx.Icon{ViewBox: 24, Stroke: 1.5, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(6, 3), gfx.LineTo(14, 3), gfx.LineTo(19, 8), gfx.LineTo(19, 21), gfx.LineTo(6, 21), gfx.LineTo(6, 3), gfx.MoveTo(14, 3), gfx.LineTo(14, 8), gfx.LineTo(19, 8)},
}
var jsonIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(9, 4), gfx.LineTo(6, 4), gfx.LineTo(6, 10), gfx.LineTo(3, 12), gfx.LineTo(6, 14), gfx.LineTo(6, 20), gfx.LineTo(9, 20), gfx.MoveTo(15, 4), gfx.LineTo(18, 4), gfx.LineTo(18, 10), gfx.LineTo(21, 12), gfx.LineTo(18, 14), gfx.LineTo(18, 20), gfx.LineTo(15, 20)},
}
var markdownIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(3, 18), gfx.LineTo(3, 6), gfx.LineTo(8, 12), gfx.LineTo(13, 6), gfx.LineTo(13, 18), gfx.MoveTo(19, 6), gfx.LineTo(19, 18), gfx.MoveTo(16, 15), gfx.LineTo(19, 18), gfx.LineTo(22, 15)},
}
var codeIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(8, 7), gfx.LineTo(3, 12), gfx.LineTo(8, 17), gfx.MoveTo(16, 7), gfx.LineTo(21, 12), gfx.LineTo(16, 17), gfx.MoveTo(14, 4), gfx.LineTo(10, 20)},
}
var plusIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(12, 5), gfx.LineTo(12, 19), gfx.MoveTo(5, 12), gfx.LineTo(19, 12)},
}
var refreshIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(20, 11), gfx.LineTo(18, 6), gfx.LineTo(12, 4), gfx.LineTo(6, 6), gfx.LineTo(4, 12), gfx.LineTo(6, 18), gfx.LineTo(12, 20), gfx.LineTo(18, 18), gfx.MoveTo(20, 5), gfx.LineTo(20, 11), gfx.LineTo(14, 11)},
}
var wrapIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(4, 5), gfx.LineTo(20, 5), gfx.MoveTo(4, 10), gfx.LineTo(17, 10), gfx.LineTo(20, 12), gfx.LineTo(20, 15), gfx.LineTo(17, 17), gfx.LineTo(10, 17), gfx.MoveTo(13, 14), gfx.LineTo(10, 17), gfx.LineTo(13, 20), gfx.MoveTo(4, 17), gfx.LineTo(6, 17)},
}
var copyIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(8, 8), gfx.LineTo(20, 8), gfx.LineTo(20, 21), gfx.LineTo(8, 21), gfx.LineTo(8, 8), gfx.MoveTo(15, 4), gfx.LineTo(4, 4), gfx.LineTo(4, 16)},
}
var applyIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(5, 5), gfx.LineTo(5, 19), gfx.MoveTo(20, 12), gfx.LineTo(9, 12), gfx.MoveTo(14, 7), gfx.LineTo(9, 12), gfx.LineTo(14, 17)},
}
var exportIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(5, 15), gfx.LineTo(5, 20), gfx.LineTo(19, 20), gfx.LineTo(19, 15), gfx.MoveTo(12, 16), gfx.LineTo(12, 3), gfx.MoveTo(7, 8), gfx.LineTo(12, 3), gfx.LineTo(17, 8)},
}
var expandIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(7, 8), gfx.LineTo(12, 3), gfx.LineTo(17, 8), gfx.MoveTo(7, 16), gfx.LineTo(12, 21), gfx.LineTo(17, 16), gfx.MoveTo(4, 12), gfx.LineTo(20, 12)},
}
var collapseIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(7, 3), gfx.LineTo(12, 8), gfx.LineTo(17, 3), gfx.MoveTo(7, 21), gfx.LineTo(12, 16), gfx.LineTo(17, 21), gfx.MoveTo(4, 12), gfx.LineTo(20, 12)},
}

var themeIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 4), gfx.LineTo(17.7, 6.3), gfx.LineTo(20, 12), gfx.LineTo(17.7, 17.7),
		gfx.LineTo(12, 20), gfx.LineTo(6.3, 17.7), gfx.LineTo(4, 12), gfx.LineTo(6.3, 6.3), gfx.LineTo(12, 4),
		gfx.MoveTo(12, 4), gfx.LineTo(12, 20), gfx.MoveTo(15, 7), gfx.LineTo(15, 17), gfx.MoveTo(17, 9), gfx.LineTo(17, 15),
	},
}

var minusIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Commands: []gfx.PathCommand{gfx.MoveTo(5, 12), gfx.LineTo(19, 12)}}
var browserIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 20), gfx.LineTo(4, 20), gfx.LineTo(4, 4),
	gfx.MoveTo(4, 9), gfx.LineTo(20, 9), gfx.MoveTo(8, 6.5), gfx.LineTo(8.1, 6.5),
}}
var imageIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 4), gfx.LineTo(21, 4), gfx.LineTo(21, 20), gfx.LineTo(3, 20), gfx.LineTo(3, 4),
	gfx.MoveTo(3, 17), gfx.LineTo(9, 11), gfx.LineTo(14, 16), gfx.LineTo(17, 13), gfx.LineTo(21, 17),
	gfx.MoveTo(16, 8), gfx.LineTo(16.1, 8),
}}

var saveIcon = gfx.Icon{ViewBox: 24, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 3), gfx.LineTo(16, 3), gfx.LineTo(21, 8), gfx.LineTo(21, 21), gfx.LineTo(3, 21), gfx.LineTo(3, 3), gfx.LineTo(5, 3),
	gfx.MoveTo(7, 3), gfx.LineTo(7, 9), gfx.LineTo(16, 9), gfx.LineTo(16, 3),
	gfx.MoveTo(7, 21), gfx.LineTo(7, 14), gfx.LineTo(17, 14), gfx.LineTo(17, 21),
}}
var searchIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(17, 10), gfx.CubicTo(17, 13.87, 13.87, 17, 10, 17), gfx.CubicTo(6.13, 17, 3, 13.87, 3, 10),
	gfx.CubicTo(3, 6.13, 6.13, 3, 10, 3), gfx.CubicTo(13.87, 3, 17, 6.13, 17, 10), gfx.MoveTo(15, 15), gfx.LineTo(21, 21),
}}
var spreadsheetIcon = gfx.Icon{ViewBox: 24, Stroke: 1.5, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(4, 3), gfx.LineTo(20, 3), gfx.LineTo(20, 21), gfx.LineTo(4, 21), gfx.LineTo(4, 3),
	gfx.MoveTo(4, 8), gfx.LineTo(20, 8), gfx.MoveTo(4, 14), gfx.LineTo(20, 14), gfx.MoveTo(10, 8), gfx.LineTo(10, 21),
}}
var wordIcon = gfx.Icon{ViewBox: 24, Stroke: 1.5, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 3), gfx.LineTo(15, 3), gfx.LineTo(20, 8), gfx.LineTo(20, 21), gfx.LineTo(5, 21), gfx.LineTo(5, 3),
	gfx.MoveTo(15, 3), gfx.LineTo(15, 8), gfx.LineTo(20, 8), gfx.MoveTo(8, 12), gfx.LineTo(17, 12), gfx.MoveTo(8, 16), gfx.LineTo(15, 16),
}}
