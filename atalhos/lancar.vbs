' PetAgent — lancador (atalho da area de trabalho e inicio automatico usam ESTE arquivo).
'
' Faz o mesmo que o plugin faz em  hermes petagent iniciar :
'   1) se o boneco JA esta na tela, nao abre um segundo (idempotente)
'   2) sobe pelo explorer.exe -> contexto NAO-ELEVADO do usuario. Isso nao e detalhe:
'      o leitor de e-mail usa COM do Outlook e o COM falha quando quem chama esta
'      elevado (foi assim que aprendemos)
'   3) sem janela de console
' E deixa rastro em %LOCALAPPDATA%\petagent\atalho.log — se um dia "nao subiu",
' a resposta esta la, nao na memoria de ninguem.
'
' ASCII puro de proposito: o host de script le .vbs como ANSI e acento quebra o parse.

Option Explicit
Dim sh, fso, exePath, logPath, ja, qtd
Set sh = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

exePath = sh.ExpandEnvironmentStrings("%LOCALAPPDATA%") & "\petagent\petagent.exe"
logPath = sh.ExpandEnvironmentStrings("%LOCALAPPDATA%") & "\petagent\atalho.log"

If Not fso.FileExists(exePath) Then
  Registra "ERRO: nao encontrei " & exePath & " (rode: hermes petagent instalar)"
  WScript.Quit 1
End If

' ja esta rodando?
ja = False
qtd = 0
On Error Resume Next
Dim wmi, procs
Set wmi = GetObject("winmgmts:\\.\root\cimv2")
Set procs = wmi.ExecQuery("SELECT ProcessId FROM Win32_Process WHERE Name='petagent.exe'")
If Err.Number = 0 Then
  qtd = procs.Count
  ja = (qtd > 0)
End If
Err.Clear
On Error GoTo 0

If ja Then
  Registra "ja estava na tela (" & qtd & " processo(s)) - nada a fazer"
Else
  On Error Resume Next
  sh.Run "explorer.exe """ & exePath & """", 0, False
  If Err.Number <> 0 Then
    Registra "ERRO ao subir: " & Err.Description
    Err.Clear
  Else
    Registra "boneco subiu (via explorer.exe, contexto do usuario - nao-elevado)"
  End If
  On Error GoTo 0
End If

Sub Registra(texto)
  On Error Resume Next
  Dim f
  Set f = fso.OpenTextFile(logPath, 8, True)
  f.WriteLine Now & "  " & texto
  f.Close
End Sub
