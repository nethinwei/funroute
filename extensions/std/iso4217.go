package std

import (
	"strings"

	"github.com/nethinwei/funroute"
)

// ISO4217 is the ISO 4217 table of active currencies and their minor-unit
// places, as a starting point for DeclareMoney, in code order. It is a copy
// the host owns: channels disagree with the standard (Stripe treats ISK as
// having no minor unit and HUF and TWD as whole amounts in places), so a host
// adjusts the entries it settles differently before declaring them. Funds and
// precious metals are left out; a host that needs one adds it.
func ISO4217() []funroute.CurrencySpec {
	out := make([]funroute.CurrencySpec, len(iso4217Codes))
	for i, code := range iso4217Codes {
		digits, listed := iso4217Places[code]
		if !listed {
			digits = 2
		}
		out[i] = funroute.CurrencySpec{Code: code, Digits: digits}
	}
	return out
}

var iso4217Codes = strings.Fields(`
	AED AFN ALL AMD ANG AOA ARS AUD AWG AZN BAM BBD BDT BGN BHD BIF BMD BND BOB BRL
	BSD BTN BWP BYN BZD CAD CDF CHF CLP CNY COP CRC CUP CVE CZK DJF DKK DOP DZD EGP
	ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ GYD HKD HNL HTG HUF IDR ILS INR
	IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL
	LYD MAD MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MYR MZN NAD NGN NIO NOK NPR
	NZD OMR PAB PEN PGK PHP PKR PLN PYG QAR RON RSD RUB RWF SAR SBD SCR SDG SEK SGD
	SHP SLE SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND TOP TRY TTD TWD TZS UAH UGX
	USD UYU UZS VES VND VUV WST XAF XCD XOF XPF YER ZAR ZMW ZWG
`)

// iso4217Places are the currencies whose minor unit is not a hundredth.
var iso4217Places = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0,
	"RWF": 0, "UGX": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}
