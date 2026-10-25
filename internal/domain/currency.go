package domain

import "strings"

// CurrencyExponent returns the number of digits of the minor unit of an
// active ISO 4217 currency (2 for EUR: cents; 0 for JPY; 3 for KWD) and
// whether the code is one: Money amounts are in minor units, so formatting
// them needs it. Unknown codes are refused by Settings.Validate.
func CurrencyExponent(code string) (int, bool) {
	if len(code) != 3 || strings.Contains(code, " ") {
		return 0, false
	}

	word := " " + code + " "

	switch {
	case strings.Contains(exponent0, word):
		return 0, true
	case strings.Contains(exponent3, word):
		return 3, true
	case strings.Contains(exponent4, word):
		return 4, true
	case strings.Contains(exponent2, word):
		return 2, true
	}

	return 0, false
}

// The active ISO 4217 currency codes by the exponent of their minor unit,
// each list space-separated and matched as a whole word.
const (
	exponent0 = " BIF CLP DJF GNF ISK JPY KMF KRW PYG RWF UGX UYI VND VUV XAF XOF XPF "
	exponent3 = " BHD IQD JOD KWD LYD OMR TND "
	exponent4 = " CLF UYW "
	exponent2 = " AED AFN ALL AMD AOA ARS AUD AWG AZN BAM BBD BDT BGN BMD BND BOB BOV BRL BSD BTN BWP BYN BZD " +
		"CAD CDF CHE CHF CHW CNY COP COU CRC CUP CVE CZK DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP " +
		"GMD GTQ GYD HKD HNL HTG HUF IDR ILS INR IRR JMD KES KGS KHR KPW KYD KZT LAK LBP LKR LRD LSL MAD MDL " +
		"MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MXV MYR MZN NAD NGN NIO NOK NPR NZD PAB PEN PGK PHP PKR PLN " +
		"QAR RON RSD RUB SAR SBD SCR SDG SEK SGD SHP SLE SOS SRD SSP STN SVC SYP SZL THB TJS TMT TOP TRY TTD " +
		"TWD TZS UAH USD USN UYU UZS VED VES WST XCD XCG YER ZAR ZMW ZWG "
)
