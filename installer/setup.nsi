; Pico Dashboard – Installer
; Bauen:  makensis -DVERSION=2.0.0 -DEXE=..\dist\PicoDashboard.exe -DOUT=..\dist\PicoDashboard-Setup.exe installer\setup.nsi
Unicode true
!include "MUI2.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef EXE
  !define EXE "..\dist\PicoDashboard.exe"
!endif
!ifndef OUT
  !define OUT "..\dist\PicoDashboard-Setup.exe"
!endif

!define APPNAME "Pico Dashboard"
!define APPEXE "PicoDashboard.exe"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\PicoDashboard"
!define RUNKEY "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${APPNAME}"
OutFile "${OUT}"
InstallDir "$PROGRAMFILES64\PicoDashboard"
InstallDirRegKey HKLM "${UNINSTKEY}" "InstallLocation"
RequestExecutionLevel admin
SetCompressor /SOLID lzma
BrandingText "${APPNAME} ${VERSION}"
ShowInstDetails nevershow

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1031 "ProductName" "${APPNAME}"
VIAddVersionKey /LANG=1031 "FileDescription" "${APPNAME} Setup"
VIAddVersionKey /LANG=1031 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1031 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1031 "LegalCopyright" "DIY-Projekt"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "sidebar.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "sidebar.bmp"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "Willkommen bei ${APPNAME}"
!define MUI_WELCOMEPAGE_TEXT "Dieser Assistent installiert ${APPNAME} ${VERSION}.$\r$\n$\r$\nDie App verbindet dein Dashboard mit Windows und hält auch die Software auf dem Pico automatisch aktuell.$\r$\n$\r$\nFalls ${APPNAME} gerade läuft, wird es kurz beendet."
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "${APPNAME} jetzt starten"
!define MUI_FINISHPAGE_RUN_FUNCTION StartApp

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "German"

Var IsUpdate

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "${APPNAME} benötigt ein 64-Bit-Windows."
    Abort
  ${EndIf}
  SetRegView 64
  ; /UPDATE = Aufruf durch den Updater der App
  StrCpy $IsUpdate "0"
  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/UPDATE" $R1
  ${IfNot} ${Errors}
    StrCpy $IsUpdate "1"
  ${EndIf}
FunctionEnd

Function un.onInit
  SetRegView 64
FunctionEnd

; Programm ohne Administratorrechte starten (über den Explorer)
Function StartApp
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\${APPEXE}"'
FunctionEnd

Function .onInstSuccess
  ${If} ${Silent}
    Call StartApp
  ${EndIf}
FunctionEnd

Section "Programm" SecMain
  SectionIn RO
  nsExec::Exec 'taskkill /F /IM ${APPEXE}'
  Sleep 800

  SetOutPath "$INSTDIR"
  File "${EXE}"
  File "..\icon.ico"

  ; Reste der Version 1 entfernen (Pico-Dateien werden jetzt von der App verwaltet)
  RMDir /r "$INSTDIR\Pico-Dateien"
  Delete "$INSTDIR\ANLEITUNG.txt"
  Delete "$SMPROGRAMS\${APPNAME}\Einstellungen.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\Pico-Dateien.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\Anleitung.lnk"

  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk" "$INSTDIR\${APPEXE}" "--open" "$INSTDIR\icon.ico"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME} (Diagnose).lnk" "$INSTDIR\${APPEXE}" "--open --debug" "$INSTDIR\icon.ico"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\Deinstallieren.lnk" "$INSTDIR\Uninstall.exe"

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "DIY-Projekt"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\icon.ico"
  WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINSTKEY}" "EstimatedSize" "$0"
SectionEnd

Section "Mit Windows starten" SecAutostart
  ; Bei automatischen Updates die Einstellung des Benutzers nicht überschreiben
  ${If} $IsUpdate == "0"
    WriteRegStr HKCU "${RUNKEY}" "PicoDashboard" '"$INSTDIR\${APPEXE}"'
  ${EndIf}
SectionEnd

Section /o "Desktop-Symbol" SecDesktop
  CreateShortcut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\${APPEXE}" "--open" "$INSTDIR\icon.ico"
SectionEnd

LangString DESC_Main ${LANG_GERMAN} "Die App mit eingebauter Dashboard-Firmware und die Einträge im Startmenü."
LangString DESC_Auto ${LANG_GERMAN} "Startet ${APPNAME} bei der Anmeldung im Infobereich (empfohlen)."
LangString DESC_Desk ${LANG_GERMAN} "Legt eine Verknüpfung auf dem Desktop an."
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecMain} $(DESC_Main)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecAutostart} $(DESC_Auto)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} $(DESC_Desk)
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Section "Uninstall"
  nsExec::Exec 'taskkill /F /IM ${APPEXE}'
  Sleep 800
  DeleteRegValue HKCU "${RUNKEY}" "PicoDashboard"
  Delete "$DESKTOP\${APPNAME}.lnk"
  RMDir /r "$SMPROGRAMS\${APPNAME}"
  RMDir /r "$INSTDIR\Pico-Dateien"
  Delete "$INSTDIR\${APPEXE}"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\ANLEITUNG.txt"
  Delete "$INSTDIR\Uninstall.exe"
  SetOutPath "$TEMP"
  RMDir "$INSTDIR"
  ; Zwischenspeicher (WebView2, Downloads) entfernen – Einstellungen in %APPDATA% bleiben erhalten
  RMDir /r "$LOCALAPPDATA\PicoDashboard"
  DeleteRegKey HKLM "${UNINSTKEY}"
SectionEnd
