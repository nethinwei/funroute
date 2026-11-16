package api_test

import (
	"fmt"
	"log"

	"github.com/nethinwei/funroute"
)

// Decimal shows the amount alone; String includes its currency.
func ExampleMoney_Decimal() {
	table, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}}})
	if err != nil {
		log.Fatal(err)
	}
	amount, err := table.Of("USD", "1.70")
	if err != nil {
		log.Fatal(err)
	}
	text, err := amount.Decimal()
	if err != nil {
		log.Fatal(err)
	}
	currency, err := table.Currency("USD")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(amount.String())
	fmt.Println(text)
	fmt.Println(currency.String())
	// Output:
	// USD 1.70
	// 1.70
	// USD
}

// Decimal rounds the display while String preserves the exact fraction.
func ExampleRatio_Decimal() {
	ratio, err := funroute.ParseRatio("1/3")
	if err != nil {
		log.Fatal(err)
	}
	text, err := ratio.Decimal(4)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)
	fmt.Println(ratio.String())
	// Output:
	// 0.3333
	// 1/3
}
