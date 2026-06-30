Unicode True
!include "MUI2.nsh"

; Passed in from CI: makensis /DVERSION=v0.14.5 /DOUTDIR=C:\...\workspace cove.nsi
; Falls back to sensible defaults for local testing.
!ifndef VERSION
  !define VERSION "dev"
!endif
; OUTDIR lets CI write the installer to the repo root instead of the script dir.
!ifndef OUTDIR
  !define OUTDIR "."
!endif

!define APP_NAME    "Cove"
!define PUBLISHER   "coveninja"
!define REG_UNINST  "Software\Microsoft\Windows\CurrentVersion\Uninstall\Cove"

Name      "${APP_NAME}"
OutFile   "${OUTDIR}\cove-windows-amd64-setup.exe"
InstallDir "$PROGRAMFILES64\Cove"
InstallDirRegKey HKCU "Software\Cove" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

; ── Pages ─────────────────────────────────────────────────────────────────────
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY

Var StartMenuFolder
!define MUI_STARTMENUPAGE_REGISTRY_ROOT      "HKCU"
!define MUI_STARTMENUPAGE_REGISTRY_KEY       "Software\Cove"
!define MUI_STARTMENUPAGE_REGISTRY_VALUENAME "StartMenuFolder"
!insertmacro MUI_PAGE_STARTMENU Application $StartMenuFolder

!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; ── Install ───────────────────────────────────────────────────────────────────
Section "-Core" SecCore
  SectionIn RO

  SetOutPath "$INSTDIR"
  ; All files are pre-assembled in staging\ by the CI package job.
  ; This includes: cove.exe, cove_shell.exe, mpv-2.dll, Qt DLLs, and web\.
  !cd "..\..\staging"
  File /r "*"
  !cd "..\packaging\windows"

  WriteRegStr HKCU "Software\Cove" "InstallDir" "$INSTDIR"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  WriteRegStr   HKLM "${REG_UNINST}" "DisplayName"    "${APP_NAME}"
  WriteRegStr   HKLM "${REG_UNINST}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr   HKLM "${REG_UNINST}" "InstallLocation" "$INSTDIR"
  WriteRegStr   HKLM "${REG_UNINST}" "Publisher"       "${PUBLISHER}"
  WriteRegStr   HKLM "${REG_UNINST}" "DisplayVersion"  "${VERSION}"
  WriteRegDWORD HKLM "${REG_UNINST}" "NoModify"       1
  WriteRegDWORD HKLM "${REG_UNINST}" "NoRepair"       1

  !insertmacro MUI_STARTMENU_WRITE_BEGIN Application
    CreateDirectory "$SMPROGRAMS\$StartMenuFolder"
    CreateShortcut  "$SMPROGRAMS\$StartMenuFolder\${APP_NAME}.lnk" \
                    "$INSTDIR\cove_shell.exe" \
                    '--backend "$INSTDIR\cove.exe" --webroot "$INSTDIR\web"' \
                    "$INSTDIR\cove_shell.exe"
    CreateShortcut  "$SMPROGRAMS\$StartMenuFolder\Uninstall ${APP_NAME}.lnk" \
                    "$INSTDIR\uninstall.exe"
  !insertmacro MUI_STARTMENU_WRITE_END
SectionEnd

Section "Desktop shortcut" SecDesktop
  CreateShortcut "$DESKTOP\${APP_NAME}.lnk" \
                 "$INSTDIR\cove_shell.exe" \
                 '--backend "$INSTDIR\cove.exe" --webroot "$INSTDIR\web"' \
                 "$INSTDIR\cove_shell.exe"
SectionEnd

; ── Uninstall ─────────────────────────────────────────────────────────────────
Section "Uninstall"
  RMDir /r "$INSTDIR\web"
  Delete "$INSTDIR\*.exe"
  Delete "$INSTDIR\*.dll"
  ; Qt platform/imageformat/etc plugin subdirectories left by windeployqt
  RMDir /r "$INSTDIR\platforms"
  RMDir /r "$INSTDIR\imageformats"
  RMDir /r "$INSTDIR\iconengines"
  RMDir /r "$INSTDIR\styles"
  RMDir /r "$INSTDIR\tls"
  RMDir /r "$INSTDIR\translations"
  RMDir /r "$INSTDIR\resources"

  !insertmacro MUI_STARTMENU_GETFOLDER Application $StartMenuFolder
  Delete "$SMPROGRAMS\$StartMenuFolder\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\$StartMenuFolder\Uninstall ${APP_NAME}.lnk"
  RMDir  "$SMPROGRAMS\$StartMenuFolder"

  Delete "$DESKTOP\${APP_NAME}.lnk"

  DeleteRegKey HKCU "Software\Cove"
  DeleteRegKey HKLM "${REG_UNINST}"

  ; Remove install dir only if empty (won't remove user-added files).
  RMDir "$INSTDIR"
SectionEnd
