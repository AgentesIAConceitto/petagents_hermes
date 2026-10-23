package main

import "testing"

// TestSaldoOpenRouter — a OpenRouter devolve o comprado e o usado; o saldo é a diferença.
// Antes só a DeepSeek era lida e, com só OPENROUTER_API_KEY no .env, o chat mostrava
// "chave não encontrada" para sempre (22/10/2026).
func TestSaldoOpenRouter(t *testing.T) {
	casos := map[string]string{
		`{"data":{"total_credits":10,"total_usage":5.18}}`:       "US$ 4.82",
		`{"data":{"total_credits":25.5,"total_usage":0}}`:        "US$ 25.50",
		`{"data":{"total_credits":10}}`:                          "Saldo indisponível (resposta inesperada do provedor)",
		`{"error":{"code":401,"message":"No auth credentials"}}`: "Saldo indisponível (resposta inesperada do provedor)",
		`não é json`: "Saldo indisponível (resposta inesperada do provedor)",
	}
	for corpo, quer := range casos {
		if got := lerSaldoOpenRouter([]byte(corpo)); got != quer {
			t.Errorf("lerSaldoOpenRouter(%s) = %q, queria %q", corpo, got, quer)
		}
	}
}

// TestSaldoDeepSeek — o formato antigo continua: USD primeiro, senão a primeira moeda.
func TestSaldoDeepSeek(t *testing.T) {
	casos := map[string]string{
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"30.00"},{"currency":"USD","total_balance":"4.82"}]}`: "US$ 4.82",
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"30.00"}]}`:                                           "CNY 30.00",
		`{"is_available":false,"balance_infos":[]}`:                                                                                    "Saldo indisponível (resposta inesperada do provedor)",
	}
	for corpo, quer := range casos {
		if got := lerSaldoDeepSeek([]byte(corpo)); got != quer {
			t.Errorf("lerSaldoDeepSeek(%s) = %q, queria %q", corpo, got, quer)
		}
	}
}
