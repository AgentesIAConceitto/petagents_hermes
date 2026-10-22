package main

import "testing"

// TestLimpaTranscricao — o Whisper "ouve" legenda no silêncio; isso não pode virar mensagem.
func TestLimpaTranscricao(t *testing.T) {
	casos := map[string]string{
		"  Legendas pela comunidade Amara.org ": "",
		"Obrigado.":                             "",
		"obrigado":                              "",
		"":                                      "",
		"Obrigado pela ajuda com o relatório.":  "Obrigado pela ajuda com o relatório.",
		" Qual é a previsão do tempo amanhã? ": "Qual é a previsão do tempo amanhã?",
	}
	for entrada, quer := range casos {
		if got := limpaTranscricao(entrada); got != quer {
			t.Errorf("limpaTranscricao(%q) = %q, queria %q", entrada, got, quer)
		}
	}
}

func TestExtensaoAudio(t *testing.T) {
	casos := map[string]string{
		"audio/webm;codecs=opus": "webm",
		"audio/ogg;codecs=opus":  "ogg",
		"audio/mp4":              "m4a",
		"audio/wav":              "wav",
		"":                       "webm",
	}
	for mime, quer := range casos {
		if got := extensaoAudio(mime); got != quer {
			t.Errorf("extensaoAudio(%q) = %q, queria %q", mime, got, quer)
		}
	}
}
