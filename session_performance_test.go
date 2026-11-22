package main

import (
	"strings"
	"testing"
)

func BenchmarkSessionPersistence(b *testing.B) {
	for _, action := range []string{"Capture", "SaveUnchanged", "SaveChanged"} {
		b.Run(action, func(b *testing.B) {
			store, err := openSessionStore(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			m := newEditorApp()
			store.attach(m)
			content := strings.Repeat("configuration=value 中文\n", 40000)
			for index := 0; index < 4; index++ {
				m.addTab(document{saved: content, eol: "LF"}, "large.txt")
			}
			if err = store.save(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				if action == "Capture" {
					store.capture()
					continue
				}
				if action == "SaveChanged" {
					m.filesSplit.Pos = float32(252 + index%2)
				}
				if err = store.save(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
