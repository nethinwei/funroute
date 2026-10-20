package std

import (
	"strings"

	"github.com/nethinwei/funroute"
)

// ISO4217 is the ISO 4217 table of active currencies and their minor-unit
// places (SIX's list one of 2026-09-17), as a starting point for
// DeclareMoney, in code order. It is a copy
// the host owns: channels disagree with the standard (Stripe treats ISK as
// having no minor unit and HUF and TWD as whole amounts in places), so a host
// adjusts the entries it settles differently before declaring them. Funds and
// precious metals are left out; a host that needs one adds it.
func ISO4217() []funroute.CurrencySpec {
	out := make([]funroute.CurrencySpec, len(iso4217Codes))
	for i, entry := range iso4217Codes {
		code, places, odd := strings.Cut(entry, ":")
		digits := 2
		if odd {
			digits = int(places[0] - '0')
		}
		out[i] = funroute.CurrencySpec{Code: code, Digits: digits}
	}
	return out
}

// iso4217Codes is every code, in code order, and its places after a colon
// where the minor unit is not a hundredth: JPY:0, KWD:3. One list, so no
// code can have places listed apart from it.
var iso4217Codes = strings.Fields(`
	AED AFN ALL AMD AOA ARS AUD AWG AZN BAM BBD BDT BHD:3 BIF:0 BMD BND BOB BRL BSD BTN
	BWP BYN BZD CAD CDF CHF CLP:0 CNY COP CRC CUP CVE CZK DJF:0 DKK DOP DZD EGP ERN ETB
	EUR FJD FKP GBP GEL GHS GIP GMD GNF:0 GTQ GYD HKD HNL HTG HUF IDR ILS INR IQD:3 IRR
	ISK:0 JMD JOD:3 JPY:0 KES KGS KHR KMF:0 KPW KRW:0 KWD:3 KYD KZT LAK LBP LKR LRD LSL LYD:3 MAD
	MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MYR MZN NAD NGN NIO NOK NPR NZD OMR:3
	PAB PEN PGK PHP PKR PLN PYG:0 QAR RON RSD RUB RWF:0 SAR SBD SCR SDG SEK SGD SHP SLE
	SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND:3 TOP TRY TTD TWD TZS UAH UGX:0 USD UYU
	UZS VED VES VND:0 VUV:0 WST XAF:0 XCD XCG XOF:0 XPF:0 YER ZAR ZMW ZWG
`)
