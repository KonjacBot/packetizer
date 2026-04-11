package protodef_test

import (
	"testing"

	"github.com/KonjacBot/packetizer/protodef"
)

func TestParseLowerProtocolJSON(t *testing.T) {
	data := []byte(`{
		"types": {
			"varint": "native",
			"string": ["pstring", { "countType": "varint" }]
		},
		"handshaking": {
			"toServer": {
				"types": {
					"packet_set_protocol": ["container", [
						{ "name": "protocolVersion", "type": "varint" },
						{ "anon": true, "type": ["switch", {
							"compareToValue": true,
							"fields": {
								"true": "void"
							}
						}]}
					]],
					"packet": ["container", [
						{ "name": "name", "type": ["mapper", {
							"type": "varint",
							"mappings": { "0x00": "set_protocol" }
						}] },
						{ "name": "params", "type": ["switch", {
							"compareTo": "name",
							"fields": { "set_protocol": "packet_set_protocol" }
						}] }
					]]
				}
			}
		}
	}`)

	doc, err := protodef.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	ir, err := protodef.Lower(doc)
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]struct{}, len(ir.Definitions))
	for _, def := range ir.Definitions {
		names[def.Name] = struct{}{}
	}
	for _, name := range []string{"VarInt", "String", "HandshakingToServerPacketSetProtocol", "HandshakingToServerPacket"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("missing definition %s", name)
		}
	}
}
