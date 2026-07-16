package context

import "testing"

func TestResolveResourceOrdinal(t *testing.T) {
	resources := []string{"a", "b", "c", "d"}
	for request, expected := range map[string]string{"把第三个隔离": "c", "第 2 项": "b", "最后一个恢复": "d"} {
		got, _, err := ResolveResource(request, resources)
		if err != nil || got != expected {
			t.Fatalf("%q => %q %v", request, got, err)
		}
	}
	if _, _, err := ResolveResource("处理一下", resources); err == nil {
		t.Fatal("ambiguous reference accepted")
	}
	if _, _, err := ResolveResource("第9个", resources); err == nil {
		t.Fatal("out-of-range reference accepted")
	}
}
