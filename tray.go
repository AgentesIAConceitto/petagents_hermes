package main

import (
	"embed"
	"io/fs"

	"github.com/getlantern/systray"
)

//go:embed all:assets
var assetsFS embed.FS

// runTray — ícone na bandeja (rodar em goroutine).
func runTray() {
	systray.Run(func() {
		if icon, err := fs.ReadFile(assetsFS, "assets/systray.ico"); err == nil {
			systray.SetIcon(icon)
		}
		systray.SetTitle("PetAgent")
		systray.SetTooltip("PetAgent: seus agentes na área de trabalho")
		mShow := systray.AddMenuItem("Mostrar bonecos", "Mostra os bonecos na tela")
		mHide := systray.AddMenuItem("Esconder tudo", "Oculta os bonecos (os agentes continuam funcionando)")
		mStatus := systray.AddMenuItem("Status", "Mostra um resumo dos agentes")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Sair", "Encerra o PetAgent")
		go func() {
			for {
				select {
				case <-mShow.ClickedCh:
					if inst != nil {
						inst.ShowAll()
					}
				case <-mHide.ClickedCh:
					if inst != nil {
						inst.HideAll()
					}
				case <-mStatus.ClickedCh:
					if inst != nil {
						inst.ShowStatus()
					}
				case <-mQuit.ClickedCh:
					if inst != nil {
						inst.QuitApp()
					}
					return
				}
			}
		}()
	}, func() {})
}
