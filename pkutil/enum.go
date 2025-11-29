package pkutil

import pk "git.konjactw.dev/falloutBot/go-mc/net/packet"

type Enum interface {
	Interface(data int32) pk.Field
}
