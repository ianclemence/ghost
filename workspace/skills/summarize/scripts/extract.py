import sys
import os
import subprocess

def extract_text(file_path):
    ext = os.path.splitext(file_path)[1].lower()
    
    if ext == '.pdf':
        return extract_pdf(file_path)
    elif ext == '.docx':
        return extract_docx(file_path)
    else:
        return f"Unsupported file type: {ext}"

def extract_pdf(file_path):
    # Try pdftotext (standard linux tool)
    try:
        result = subprocess.run(['pdftotext', file_path, '-'], capture_output=True, text=True)
        if result.returncode == 0:
            return result.stdout
    except FileNotFoundError:
        pass

    # Try pypdf python library
    try:
        from pypdf import PdfReader
        reader = PdfReader(file_path)
        text = ""
        for page in reader.pages:
            text += page.extract_text() + "\n"
        return text
    except ImportError:
        pass

    return "Error: Could not extract PDF. Please install 'poppler-utils' (sudo apt install poppler-utils) or 'pypdf' (pip install pypdf)."

def extract_docx(file_path):
    # Try pandoc
    try:
        result = subprocess.run(['pandoc', '-f', 'docx', '-t', 'plain', file_path], capture_output=True, text=True)
        if result.returncode == 0:
            return result.stdout
    except FileNotFoundError:
        pass

    # Try python-docx
    try:
        import docx
        doc = docx.Document(file_path)
        return "\n".join([para.text for param in doc.paragraphs])
    except ImportError:
        pass

    return "Error: Could not extract DOCX. Please install 'pandoc' (sudo apt install pandoc) or 'python-docx' (pip install python-docx)."

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python extract.py <file_path>")
        sys.exit(1)
    
    path = sys.argv[1]
    if not os.path.exists(path):
        print(f"Error: File not found: {path}")
        sys.exit(1)
        
    print(extract_text(path))
