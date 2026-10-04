@echo off
rem PetAgent — PARADA DE EMERGENCIA
rem Duplo clique aqui mata o overlay imediatamente.
taskkill /IM petagent.exe /F
echo.
echo PetAgent encerrado.
timeout /t 3 > nul
