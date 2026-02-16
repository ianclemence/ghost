# File Organizer Skill

## Description
This skill helps the user organize cluttered directories by sorting files into subfolders based on their file extensions (e.g., Images, Documents, Videos).

## Capabilities
- **Analyze**: Scan a directory and list file types.
- **Sort**: Move files into categories:
    - `Images`: .jpg, .png, .gif, .bmp
    - `Documents`: .pdf, .docx, .txt, .md, .xlsx
    - `Archives`: .zip, .rar, .7z, .tar
    - `Audio`: .mp3, .wav, .flac
    - `Video`: .mp4, .mkv, .avi
    - `Scripts`: .py, .js, .ps1, .sh
    - `Others`: Anything else.

## Usage Guidelines
1.  **Safety First**:
    - Always `ls` the directory first to confirm it's the right one.
    - **Never** organize the root of a project or system folders (like `C:\Windows` or the project root) without explicit confirmation.
    - Prefer organizing specific "Downloads" or "Desktop" or "Temp" folders.

2.  **Execution**:
    - Use PowerShell to create directories if they don't exist.
    - Move files safely.

## Cross-Platform Method (Python)
Preferred for Linux, Mac, and complex organization tasks.

1.  **Run Script**:
    ```bash
    python workspace/skills/organizer/scripts/organize.py "/path/to/directory"
    ```

## Example PowerShell Workflow
```powershell
# 1. Check target
$Target = "C:\Path\To\MessyFolder"
Get-ChildItem $Target

# 2. Create folders (example)
New-Item -ItemType Directory -Force -Path "$Target\Images"
New-Item -ItemType Directory -Force -Path "$Target\Documents"

# 3. Move files (example)
Move-Item "$Target\*.jpg" "$Target\Images\"
Move-Item "$Target\*.pdf" "$Target\Documents\"
```
