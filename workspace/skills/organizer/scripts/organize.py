import os
import shutil
import sys

EXTENSIONS = {
    'Images': ['.jpg', '.jpeg', '.png', '.gif', '.bmp', '.svg'],
    'Documents': ['.pdf', '.docx', '.doc', '.txt', '.md', '.xlsx', '.csv'],
    'Archives': ['.zip', '.rar', '.7z', '.tar', '.gz'],
    'Audio': ['.mp3', '.wav', '.flac', '.aac'],
    'Video': ['.mp4', '.mkv', '.avi', '.mov'],
    'Scripts': ['.py', '.js', '.ps1', '.sh', '.bat']
}

def organize(target_dir):
    if not os.path.exists(target_dir):
        print(f"Directory not found: {target_dir}")
        return

    print(f"Organizing: {target_dir}")
    for filename in os.listdir(target_dir):
        file_path = os.path.join(target_dir, filename)
        if os.path.isfile(file_path):
            ext = os.path.splitext(filename)[1].lower()
            moved = False
            for category, exts in EXTENSIONS.items():
                if ext in exts:
                    cat_dir = os.path.join(target_dir, category)
                    os.makedirs(cat_dir, exist_ok=True)
                    try:
                        shutil.move(file_path, os.path.join(cat_dir, filename))
                        print(f"Moved {filename} -> {category}")
                    except Exception as e:
                        print(f"Error moving {filename}: {e}")
                    moved = True
                    break
            
            if not moved:
                # Optional: Move to 'Others' or leave as is
                pass

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python organize.py <directory>")
    else:
        organize(sys.argv[1])
