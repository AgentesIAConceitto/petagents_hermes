' Le a caixa de e-mail do Outlook SEM NENHUMA JANELA.
' wscript.exe e um host sem console; WshShell.Run com 0 = janela oculta.
' Assim nada pisca na tela do usuario a cada varredura.
Dim sh
Set sh = CreateObject("WScript.Shell")
sh.Run "powershell.exe -NoProfile -ExecutionPolicy Bypass -File ""C:\petagent_mail\poll.ps1""", 0, False
