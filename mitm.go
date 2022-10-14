package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/andybalholm/brotli"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type VocabMasterHandler struct {
	proxy.BaseAddon
}

var infoLabel = widget.NewLabel("")

func (c *VocabMasterHandler) Request(f *proxy.Flow) {
	if f.Request.URL.Host != "app.vocabgo.com" {
		return
	}
	if strings.Contains(f.Request.URL.Path, "/student/api/Student/ClassTask/SubmitChoseWord") {
		//Fulsh word storage and wordlist
		words = []WordInfo{}
		dataset.CurrentTask.WordList = []string{}

		//Put wordlist into dataset
		wordListRegex := regexp.MustCompile(`\[.*?\]`)
		wordListRaw := wordListRegex.FindString(string(f.Request.Body))
		json.Unmarshal([]byte(wordListRaw), &dataset.CurrentTask.WordList)

		//Put task info into dataset
		taskInfoRegex := regexp.MustCompile(`"word_map":{".*?"`)
		taskInfoRaw := taskInfoRegex.FindString(string(f.Request.Body))
		taskInfo := strings.Split(taskInfoRaw[13:len(taskInfoRaw)-1], ":")
		dataset.CurrentTask.TaskID = taskInfo[0]
		dataset.CurrentTask.TaskSet = taskInfo[1]

		//Update Cookie and Header
		dataset.RequestInfo.Cookies = f.Request.Raw().Cookies()
		dataset.RequestInfo.Header = f.Request.Raw().Header

		//Create a thread to crawl all words
		go func() {
			//Setup progress ui
			progressBar := widget.NewProgressBar()
			completeBox := widget.NewLabel("Gathering word info...")
			var wordList string
			if len(dataset.CurrentTask.WordList) > 8 {
				wordList = fmt.Sprintln(dataset.CurrentTask.WordList[:8]) + "..."
			} else {
				wordList = fmt.Sprintln(dataset.CurrentTask.WordList)
			}
			infoBox := widget.NewLabel("New task detected. Gathering chosen words' info:\n" + wordList)
			window.SetContent(container.NewVBox(infoBox, progressBar, completeBox, infoLabel))

			for i := 0; i < len(dataset.CurrentTask.WordList); i++ {
				//Show progress
				progressBar.SetValue(float64(i) / float64(len(dataset.CurrentTask.WordList)))
				grabWord(dataset.CurrentTask.WordList[i])
				log.Println("[I] Grabbed word list:" + dataset.CurrentTask.WordList[i])
			}
			progressBar.SetValue(1)
			completeBox.SetText("Complete!")
		}()
	}

}

func (c *VocabMasterHandler) Response(f *proxy.Flow) {
	//Judge whether session is from vocabgo task
	if f.Request.URL.Host != "app.vocabgo.com" {
		return
	}
	if !strings.Contains(f.Request.URL.Path, "/student/api/Student/ClassTask/SubmitAnswerAndSave") && !strings.Contains(f.Request.URL.Path, "/student/api/Student/ClassTask/StartAnswer") {
		return
	}

	//Get decoded content
	rawByte, _ := f.Response.DecodedBody()

	//Okay! Let's decode raw json
	var vocabRawJSON VocabRawJSONStruct
	json.Unmarshal(rawByte, &vocabRawJSON)

	//Judge whether is the last task
	if vocabRawJSON.Msg != "success" {
		return
	}

	//JSON Salt
	JSONSalt := vocabRawJSON.Data[:32]
	rawJSONBase64 := vocabRawJSON.Data[32:]

	//Let's get the insider base64-encoded info
	rawDecodedString, err := base64.StdEncoding.DecodeString(rawJSONBase64)
	if err != nil {
		panic(err)
	}

	//Judge whether json contains task info
	if !strings.Contains(string(rawDecodedString), "task_id") {
		return
	}

	var vocabTask VocabTaskStruct
	json.Unmarshal(rawDecodedString, &vocabTask)

	//Switch for tasks
	switch vocabTask.TopicMode {
	//Introducing words
	case 0:
		//UI
		infoLabel.SetText("Seems like you are entering an new task!\nPlease wait until progress bar reach 100%.")
	//Choose translation of specific word from a sentence
	case 11:
		var translation string
		var found bool
		stemConverted := strings.ReplaceAll(vocabTask.Stem.Content, "  ", " ")
		for i := 0; i < len(words); i++ {
			for j := 0; j < len(words[i].Content); j++ {
				for k := 0; k < len(words[i].Content[j].ExampleEnglish); k++ {
					if words[i].Content[j].ExampleEnglish[k] == stemConverted {
						translation = words[i].Content[j].Meaning
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}

		var contentIndex int = -1
		for i := 0; i < len(vocabTask.Options); i++ {
			regex := regexp.MustCompile(`（.*?）`)
			vocabTask.Options[i].Content = string(regex.ReplaceAll([]byte(vocabTask.Options[i].Content), []byte("")))
			if compareTranslation(translation, vocabTask.Options[i].Content) {
				contentIndex = i
				break
			}
		}

		//UI
		if found && contentIndex != -1 {
			infoLabel.SetText("Hey! The answer is tagged out.\nAnd the answer is [" + translation + "]")
		} else {
			infoLabel.SetText("Warn: Answer not found. This might be a bug.")
		}

		//Check whether index is found
		if contentIndex != -1 {
			//Tag out the correct answer
			regex := regexp.MustCompile(`（.*?）`)
			newJSON := string(rawDecodedString)
			newJSON = string(regex.ReplaceAll([]byte(newJSON), []byte("")))
			newJSON = strings.Replace(newJSON, vocabTask.Options[contentIndex].Content, "-> "+vocabTask.Options[contentIndex].Content+" <-", 1)
			//newJSON := strings.Replace(string(rawDecodedString), vocabTask.Stem.Content, vocabTask.Stem.Content+" ["+translation+"]", 1)
			repackedBase64 := base64.StdEncoding.EncodeToString([]byte(newJSON))
			vocabRawJSON.Data = JSONSalt + repackedBase64
			body, _ := json.Marshal(vocabRawJSON)
			var b bytes.Buffer
			br := brotli.NewWriter(&b)
			br.Write(body)
			br.Flush()
			br.Close()
			f.Response.Body = b.Bytes()
		}

	//Choose word from voice
	case 22:
		var contentIndex int
		var found bool
		for i := 0; i < len(words); i++ {
			if words[i].Word == vocabTask.Stem.Content {
				for j := 0; j < len(words[i].Content); j++ {
					for k := 0; k < len(vocabTask.Options); k++ {
						regex := regexp.MustCompile(`（.*?）`)
						vocabTask.Options[k].Content = string(regex.ReplaceAll([]byte(vocabTask.Options[k].Content), []byte("")))
						if compareTranslation(vocabTask.Options[k].Content, words[i].Content[j].Meaning) {
							contentIndex = k
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				break
			}
		}

		//UI
		if found && contentIndex != -1 {
			infoLabel.SetText("Hey! The answer is tagged out.\nAnd the answer is [" + vocabTask.Options[contentIndex].Content + "]")
		} else {
			infoLabel.SetText("Warn: Answer not found. This might be a bug.")
		}

		if contentIndex != -1 {
			//Tag out the correct answer
			regex := regexp.MustCompile(`（.*?）`)
			newJSON := string(rawDecodedString)
			newJSON = string(regex.ReplaceAll([]byte(newJSON), []byte("")))
			newJSON = strings.Replace(newJSON, vocabTask.Options[contentIndex].Content, "-> "+vocabTask.Options[contentIndex].Content+" <-", 1)
			repackedBase64 := base64.StdEncoding.EncodeToString([]byte(newJSON))
			vocabRawJSON.Data = JSONSalt + repackedBase64
			body, _ := json.Marshal(vocabRawJSON)
			var b bytes.Buffer
			br := brotli.NewWriter(&b)
			br.Write(body)
			br.Flush()
			br.Close()
			f.Response.Body = b.Bytes()
		}

	//Choose word pair
	case 31:
		var tag, detag []string
		for i := 0; i < len(vocabTask.Stem.Remark); i++ {
			for j := 0; j < len(vocabTask.Options); j++ {
				if strings.Contains(vocabTask.Stem.Remark[i].SenMarked, vocabTask.Options[j].Content) {
					tag = append(tag, vocabTask.Options[j].Content)
				}
			}
		}

		//Get the incorrect options
		for i := 0; i < len(vocabTask.Options[i].Content); i++ {
			var f bool
			for j := 0; j < len(tag); j++ {
				if vocabTask.Options[i].Content == tag[j] {
					f = true
					break
				}
			}
			if !f {
				detag = append(detag, vocabTask.Options[i].Content)
			}
		}

		infoLabel.SetText("The incorrect answer is tagged out, and the answer is:\n" + fmt.Sprintln(tag))

		//Show answer in the UI

		newJSON := string(rawDecodedString)
		for i := 0; i < len(detag); i++ {
			newJSON = strings.Replace(newJSON, `"content":"`+detag[i]+`"`, `"content":"`+"NOT-["+detag[i]+"]-THIS"+`"`, 1)
		}
		repackedBase64 := base64.StdEncoding.EncodeToString([]byte(newJSON))
		vocabRawJSON.Data = JSONSalt + repackedBase64
		body, _ := json.Marshal(vocabRawJSON)
		var b bytes.Buffer
		br := brotli.NewWriter(&b)
		br.Write(body)
		br.Flush()
		br.Close()
		f.Response.Body = b.Bytes()

	//Organize word pieces
	case 32:
		regexFind := regexp.MustCompile(`"remark":".*?"`)
		raw := regexFind.FindString(string(rawDecodedString))
		word := raw[10 : len(raw)-1]
		var tag string
		var found bool
		for i := 0; i < len(words); i++ {
			for j := 0; j < len(words[i].Content); j++ {
				for k := 0; k < len(words[i].Content[j].Usage); k++ {
					if strings.Contains(words[i].Content[j].Usage[k], word) {
						tag = words[i].Content[j].Usage[k]
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}

		//UI
		infoLabel.SetText("Hey! The answer is printed out.\nAnd the answer")

		//Change the hint to the correct answer
		newJSON := strings.Replace(string(rawDecodedString), word, tag, 1)
		repackedBase64 := base64.StdEncoding.EncodeToString([]byte(newJSON))
		vocabRawJSON.Data = JSONSalt + repackedBase64
		body, _ := json.Marshal(vocabRawJSON)
		var b bytes.Buffer
		br := brotli.NewWriter(&b)
		br.Write(body)
		br.Flush()
		br.Close()
		f.Response.Body = b.Bytes()

	//Write words from first chars
	case 51:
		regexFind := regexp.MustCompile(`"remark":".*?"`)
		raw := regexFind.FindString(string(rawDecodedString))
		word := raw[10 : len(raw)-1]
		var tag string
		var found bool
		for i := 0; i < len(words); i++ {
			for j := 0; j < len(words[i].Content); j++ {
				for k := 0; k < len(words[i].Content[j].Usage); k++ {
					if strings.Contains(words[i].Content[j].Usage[k], word) {
						tag = words[i].Content[j].Usage[k]
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}

		//UI
		if found {
			infoLabel.SetText("Hey! The answer is printed out. And the answer is [" + tag + "]")

			//Change the hint to the correct answer
			newJSON := strings.Replace(string(rawDecodedString), word, tag, 1)
			repackedBase64 := base64.StdEncoding.EncodeToString([]byte(newJSON))
			vocabRawJSON.Data = JSONSalt + repackedBase64
			body, _ := json.Marshal(vocabRawJSON)
			var b bytes.Buffer
			br := brotli.NewWriter(&b)
			br.Write(body)
			br.Flush()
			br.Close()
			f.Response.Body = b.Bytes()
		} else {
			infoLabel.SetText("Warn: Answer not found. This might be a bug.")
		}

	default:
		infoLabel.SetText("This task is not supported or this is a bug.\n")
	}
}

func compareTranslation(str1 string, str2 string) bool {
	//If they are actually the same, then that's good
	if str1 == str2 {
		return true
	}

	//Compare length first
	if len(str1) != len(str2) {
		return false
	}

	//Split the classification of current word and translation
	str1Slice := strings.Split(str1, " ")
	str2Slice := strings.Split(str2, " ")

	//Judge the classification of current word
	if str1Slice[0] != str2Slice[0] {
		return false
	}

	//Split
	str1 = strings.ReplaceAll(str1Slice[1], "；", "，")
	str2 = strings.ReplaceAll(str2Slice[1], "；", "，")
	str1Slice = strings.Split(str1, "，")
	str2Slice = strings.Split(str2, "，")

	//Compare split length
	if len(str1Slice) != len(str2Slice) {
		return false
	}

	//Sort and compare content
	sort.Strings(str1Slice)
	sort.Strings(str2Slice)

	for i := 0; i < len(str1Slice); i++ {
		if str1Slice[i] != str2Slice[i] {
			return false
		}
	}

	return true
}
