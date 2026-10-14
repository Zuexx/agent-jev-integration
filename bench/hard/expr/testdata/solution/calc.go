// Package calc evaluates integer arithmetic expressions.
package calc

import (
	"errors"
	"strconv"
	"unicode"
)

type parser struct {
	s   string
	pos int
}

var errSyntax = errors.New("syntax error")

func (p *parser) skip() {
	for p.pos < len(p.s) && p.s[p.pos] == ' ' {
		p.pos++
	}
}

func (p *parser) peek() byte {
	p.skip()
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func Eval(s string) (int64, error) {
	p := &parser{s: s}
	v, err := p.sum()
	if err != nil {
		return 0, err
	}
	if p.peek() != 0 {
		return 0, errSyntax
	}
	return v, nil
}

func (p *parser) sum() (int64, error) {
	v, err := p.product()
	for err == nil {
		op := p.peek()
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		var r int64
		if r, err = p.product(); err == nil {
			if op == '+' {
				v += r
			} else {
				v -= r
			}
		}
	}
	return v, err
}

func (p *parser) product() (int64, error) {
	v, err := p.unary()
	for err == nil {
		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			break
		}
		p.pos++
		var r int64
		if r, err = p.unary(); err != nil {
			break
		}
		switch {
		case op == '*':
			v *= r
		case r == 0:
			err = errors.New("division by zero")
		case op == '/':
			v /= r
		default:
			v %= r
		}
	}
	return v, err
}

func (p *parser) unary() (int64, error) {
	switch p.peek() {
	case '-':
		p.pos++
		v, err := p.unary()
		return -v, err
	case '+':
		p.pos++
		return p.unary()
	}
	return p.power()
}

func (p *parser) power() (int64, error) {
	base, err := p.atom()
	if err != nil || p.peek() != '^' {
		return base, err
	}
	p.pos++
	exp, err := p.unary() // right-assoc; allows 2^-0 and rejects negatives below
	if err != nil {
		return 0, err
	}
	if exp < 0 {
		return 0, errors.New("negative exponent")
	}
	r := int64(1)
	for i := int64(0); i < exp; i++ {
		r *= base
	}
	return r, nil
}

func (p *parser) atom() (int64, error) {
	c := p.peek()
	if c == '(' {
		p.pos++
		v, err := p.sum()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, errSyntax
		}
		p.pos++
		return v, nil
	}
	start := p.pos
	for p.pos < len(p.s) && unicode.IsDigit(rune(p.s[p.pos])) {
		p.pos++
	}
	if start == p.pos {
		return 0, errSyntax
	}
	return strconv.ParseInt(p.s[start:p.pos], 10, 64)
}
