@echo off
REM Wrapper para os modos de linha de comando do PetAgent.
REM   petagent-cli.cmd balance          -> saldo DeepSeek
REM   petagent-cli.cmd monitores        -> monitores do Windows
REM   petagent-cli.cmd galeria [termo]  -> galeria de bonecos (vazio = instalados)
REM
REM ATENCAO (medido 02/10/2026): por ser um app Wails, o WebView2 inicializa mesmo
REM nos modos de console - se o overlay estiver aberto isso TRAVA o processo, e com
REM o overlay fechado o WebView2 as vezes falha ("WebVie2wProcess failed"). Modo
REM experimental: use o painel do proprio app ou o comando do plugin (hermes petagent).
"%~dp0build\bin\petagent.exe" %*
