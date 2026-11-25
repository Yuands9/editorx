package editor

const imageOCRDirectory = "image-ocr"
const tessdataFastRevisionURL = "https://raw.githubusercontent.com/tesseract-ocr/tessdata_fast/923915d4ced2a7235221788285785a29c4a42d4a/"

var imageOCRComponent = offlineComponent{
	ID: "tesseract-5.5-1", Label: "中英文 OCR", Directory: imageOCRDirectory,
	Required: []string{
		"python.exe", "python313.dll", "python313.zip", "tesserocr/__init__.py", "tesserocr/tesserocr.cp313-win_amd64.pyd",
		"tesserocr.libs/tesseract55-a353c6df28c139558755a8cce82db953.dll",
		"tessdata/eng.traineddata", "tessdata/chi_sim.traineddata",
	},
	Archives: []offlineArchive{
		windowsEmbeddedPython,
		{"https://files.pythonhosted.org/packages/0b/92/69b37fd0be24f65fa0699a0c84b98093cc6061b71fd5175b9e748b6e3ac4/tesserocr-2.13.0-cp313-cp313-win_amd64.whl", "980236d904b58f10b4251a015d0262968e1cb51aeb00e47c608e666c32ab4027", 23167947},
	},
	Files: []offlineComponentFile{
		{"tessdata/eng.traineddata", offlineArchive{tessdataFastRevisionURL + "eng.traineddata", "7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2", 4113088}},
		{"tessdata/chi_sim.traineddata", offlineArchive{tessdataFastRevisionURL + "chi_sim.traineddata", "486251aa9fd3e972b6b99ae100a0df3eba0fc54b5f8ab5acb696252fb773026a", 2469067}},
		{"tessdata/LICENSE", offlineArchive{"https://raw.githubusercontent.com/tesseract-ocr/tesseract/5.5.3/LICENSE", "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30", 11358}},
	},
}
