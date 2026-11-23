package editor

import (
	"strings"
	"testing"
)

// Minified configuration files put many matches on one long UTF-8 line.
// Measure the actual result refresh, including finding matches and building rows.
func BenchmarkSearchResultLongLine(b *testing.B) {
	source := "[" + strings.Repeat(`{"name":"中文配置","enabled":true,"port":8080},`, 999) + `{"name":"中文配置","enabled":true,"port":8080}]`
	model := newEditorApp()
	tab := model.addTab(document{saved: source, eol: "LF"}, "minified.json")
	results := &model.results
	results.origin, results.pane, results.query = tab, tab.source, "中文"
	results.groups = nil
	results.refresh(model)
	if results.total != 1000 || len(results.rows) != 1001 {
		b.Fatalf("unexpected results: matches=%d rows=%d", results.total, len(results.rows))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results.groups = nil
		results.refresh(model)
	}
}
