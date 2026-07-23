package session

// ojson.go provides a minimal order-preserving JSON model so the installers can
// edit a user's settings.json/hooks.json without reordering the keys they did
// not touch. encoding/json into map[string]any sorts keys alphabetically,
// which would churn a user's whole settings file on every repair; this model
// preserves the original insertion order and only changes the entries it owns.
//
// It is deliberately tiny: it supports the object/array/scalar shapes these
// settings files use and round-trips numbers via json.Number so their original
// formatting survives. It is not a general JSON library.

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// jnode is an order-preserving JSON value.
type jnode struct {
	kind  byte        // 'o' object, 'a' array, 's' string, 'n' number, 'b' bool, 'z' null
	str   string      // kind == 's'
	num   json.Number // kind == 'n'
	boo   bool        // kind == 'b'
	keys  []string    // kind == 'o': insertion-ordered keys
	vals  map[string]*jnode
	items []*jnode // kind == 'a'
}

func jNull() *jnode        { return &jnode{kind: 'z'} }
func jBool(b bool) *jnode  { return &jnode{kind: 'b', boo: b} }
func jStr(s string) *jnode { return &jnode{kind: 's', str: s} }
func jNum(n json.Number) *jnode {
	return &jnode{kind: 'n', num: n}
}
func jArr(items ...*jnode) *jnode { return &jnode{kind: 'a', items: items} }
func jObj() *jnode                { return &jnode{kind: 'o', vals: map[string]*jnode{}} }

// parseJSON decodes data into an order-preserving node. An empty/missing file
// yields an empty object so callers can build on it. A non-object top level is
// rejected: every settings file these installers touch is a JSON object.
func parseJSON(data []byte) (*jnode, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return jObj(), nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	n, err := decodeNode(dec)
	if err != nil {
		return nil, err
	}
	if n.kind != 'o' {
		return nil, fmt.Errorf("expected JSON object at top level, got %q", n.kind)
	}
	return n, nil
}

// decodeNode reads one JSON value from dec, preserving object key order.
func decodeNode(dec *json.Decoder) (*jnode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		switch v := tok.(type) {
		case string:
			return jStr(v), nil
		case bool:
			return jBool(v), nil
		case json.Number:
			return jNum(v), nil
		case nil:
			return jNull(), nil
		default:
			return nil, fmt.Errorf("unexpected token %T", tok)
		}
	}
	switch delim {
	case '{':
		obj := jObj()
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := t.(string)
			if !ok {
				return nil, fmt.Errorf("expected string key, got %T", t)
			}
			val, err := decodeNode(dec)
			if err != nil {
				return nil, err
			}
			obj.set(key, val)
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return nil, err
		}
		return obj, nil
	case '[':
		arr := jArr()
		for dec.More() {
			val, err := decodeNode(dec)
			if err != nil {
				return nil, err
			}
			arr.items = append(arr.items, val)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, err
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected delim %q", delim)
}

// set assigns key=val, preserving insertion order (appends new keys).
func (n *jnode) set(key string, val *jnode) {
	if _, exists := n.vals[key]; !exists {
		n.keys = append(n.keys, key)
	}
	n.vals[key] = val
}

// get returns the value for key (objects only).
func (n *jnode) get(key string) (*jnode, bool) {
	v, ok := n.vals[key]
	return v, ok
}

// strVal returns the string value for key, ok=false when absent or non-string.
func (n *jnode) strVal(key string) (string, bool) {
	v, ok := n.vals[key]
	if !ok || v.kind != 's' {
		return "", false
	}
	return v.str, true
}

// marshal serializes to 2-space-indented JSON, preserving key order.
func (n *jnode) marshal() []byte {
	var b bytes.Buffer
	n.writeTo(&b, 0)
	b.WriteByte('\n')
	return b.Bytes()
}

func (n *jnode) writeTo(b *bytes.Buffer, depth int) {
	ind := indentFor(depth)
	next := indentFor(depth + 1)
	switch n.kind {
	case 'z':
		b.WriteString("null")
	case 'b':
		if n.boo {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case 'n':
		b.WriteString(string(n.num))
	case 's':
		out, _ := json.Marshal(n.str) // a string marshal cannot fail
		b.Write(out)
	case 'a':
		if len(n.items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, it := range n.items {
			b.Write(next)
			it.writeTo(b, depth+1)
			if i < len(n.items)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.Write(ind)
		b.WriteByte(']')
	case 'o':
		if len(n.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range n.keys {
			b.Write(next)
			out, _ := json.Marshal(k)
			b.Write(out)
			b.WriteString(": ")
			n.vals[k].writeTo(b, depth+1)
			if i < len(n.keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.Write(ind)
		b.WriteByte('}')
	}
}

func indentFor(depth int) []byte {
	return bytes.Repeat([]byte("  "), depth)
}
