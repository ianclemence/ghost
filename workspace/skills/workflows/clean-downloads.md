---
name: "clean-downloads"
description: "Weekly automated cleanup of the downloads directory."
schedule: "Friday 5pm"
category: "digital-hygiene"
---

# Digital Cleanup

Keep the user's file system tidy by dealing with the notorious Downloads folder.

## 1. Check Directory Size
Use the `filesystem` tools to list the contents of the `~/Downloads` directory (or wherever they store temporary files).
Are there more than 20 files, or files older than 30 days? 

## 2. Compile Report
Do **not** delete files automatically. Instead, create a brief report of the top largest files, or simply state how messy it is getting.

## 3. Recommend Deletion

```
💾 **Digital Hygiene Check**

Your Downloads folder currently contains [X] files taking up [Y]MB. 

Some massive files include:
- [Large file 1] (X MB)
- [Large file 2] (Y MB)

Would you like me to archive files older than 30 days, delete them, or leave them alone?
```
