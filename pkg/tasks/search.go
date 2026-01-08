package tasks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/simple"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/sirupsen/logrus"
	"github.com/xbapps/xbvr/pkg/common"
	"github.com/xbapps/xbvr/pkg/config"
	"github.com/xbapps/xbvr/pkg/models"
)

type Index struct {
	Bleve bleve.Index
}

type SceneIndexed struct {
	Description string    `json:"description"`
	Title       string    `json:"title"`
	Cast        string    `json:"cast"`
	Site        string    `json:"site"`
	Id          string    `json:"id"`
	Released    time.Time `json:"released"`
	Added       time.Time `json:"added"`
	Duration    int       `json:"duration"`
}

func NewIndex(name string) (*Index, error) {
	i := new(Index)

	path := filepath.Join(common.IndexDirV2, name)

	// the simple analyzer is more approriate for the title and cast
	// note this does not effect search unless the query includes cast: or title:
	titleFieldMapping := bleve.NewTextFieldMapping()
	titleFieldMapping.Analyzer = simple.Name
	castFieldMapping := bleve.NewTextFieldMapping()
	castFieldMapping.Analyzer = simple.Name
	releaseFieldMapping := bleve.NewDateTimeFieldMapping()
	addedFieldMapping := bleve.NewDateTimeFieldMapping()
	durationFieldMapping := bleve.NewNumericFieldMapping()
	sceneMapping := bleve.NewDocumentMapping()
	sceneMapping.AddFieldMappingsAt("title", titleFieldMapping)
	sceneMapping.AddFieldMappingsAt("cast", castFieldMapping)
	sceneMapping.AddFieldMappingsAt("released", releaseFieldMapping)
	sceneMapping.AddFieldMappingsAt("added", addedFieldMapping)
	sceneMapping.AddFieldMappingsAt("duration", durationFieldMapping)

	mapping := bleve.NewIndexMapping()
	mapping.AddDocumentMapping("_default", sceneMapping)

	idx, err := bleve.NewUsing(path, mapping, scorch.Name, scorch.Name, nil)
	if err != nil && err == bleve.ErrorIndexPathExists {
		idx, err = bleve.Open(path)
	}
	if err != nil {
		return nil, err
	}

	i.Bleve = idx
	return i, nil
}

func (i *Index) Exist(id string) bool {
	d, err := i.Bleve.Document(id)
	if err != nil || d == nil {
		return false
	}
	return true
}

func (i *Index) PutScene(scene models.Scene) error {
	cast := ""
	castConcat := ""
	for _, c := range scene.Cast {
		cast = cast + " " + c.Name
		castConcat = castConcat + " " + strings.Replace(c.Name, " ", "", -1)
	}

	rd := time.Date(scene.ReleaseDate.Year(), scene.ReleaseDate.Month(), scene.ReleaseDate.Day(), 0, 0, 0, 0, &time.Location{})
	si := SceneIndexed{
		Title:       fmt.Sprintf("%v", scene.Title),
		Description: fmt.Sprintf("%v", scene.Synopsis),
		Cast:        fmt.Sprintf("%v %v", cast, castConcat),
		Site:        fmt.Sprintf("%v", scene.Site),
		Id:          fmt.Sprintf("%v", scene.SceneID),
		Released:    rd,                                       // only index the date, not the time
		Added:       scene.CreatedAt.Truncate(24 * time.Hour), // only index the date, not the time
		Duration:    scene.Duration,
	}

	if err := i.Bleve.Index(scene.SceneID, si); err != nil {
		return err
	}

	return nil
}

func SearchIndex() {
	if !models.CheckLock("index") {
		models.CreateLock("index")
		defer models.RemoveLock("index")

		tlog := log.WithFields(logrus.Fields{"task": "scrape"})

		idx, err := NewIndex("scenes")
		if err != nil {
			log.Error(err)
			models.RemoveLock("index")
			return
		}

		db, _ := models.GetDB()
		defer db.Close()

		total := 0
		offset := 0
		current := 0
		var scenes []models.Scene
		tx := db.Model(models.Scene{}).Preload("Cast").Preload("Tags")
		tx.Count(&total)

		tlog.Infof("Building search index...")

		for {
			tx.Offset(offset).Limit(100).Find(&scenes)
			if len(scenes) == 0 {
				break
			}

			for i := range scenes {
				if !idx.Exist(scenes[i].SceneID) {
					err := idx.PutScene(scenes[i])
					if err != nil {
						log.Error(err)
					}
				}
				current = current + 1
			}
			tlog.Infof("Indexed %v/%v scenes", current, total)

			// Update migration status if migration is running
			if config.State.Migration.IsRunning {
				msg := fmt.Sprintf("Reindexing scenes: %v/%v", current, total)
				config.UpdateMigrationStatus(config.State.Migration.Current, current, total, msg)
			}

			offset = offset + 100
		}

		idx.Bleve.Close()

		tlog.Infof("Search index built!")
	}
}

/**
 * Update search index for all of the specified scenes.
 */
func IndexScenes(scenes *[]models.Scene) {
	if !models.CheckLock("index") {
		models.CreateLock("index")
		defer models.RemoveLock("index")

		tlog := log.WithFields(logrus.Fields{"task": "scrape"})

		idx, err := NewIndex("scenes")
		if err != nil {
			log.Error(err)
			models.RemoveLock("index")
			return
		}

		tlog.Infof("Adding scraped scenes to search index...")

		total := 0
		lastMessage := time.Now()
		for i := range *scenes {
			if time.Since(lastMessage) > time.Duration(config.Config.Advanced.ProgressTimeInterval)*time.Second {
				tlog.Infof("Indexed %v of %v scenes", total, len(*scenes))
				lastMessage = time.Now()
			}
			scene := (*scenes)[i]
			if idx.Exist(scene.SceneID) {
				// Remove old index, as data may have been updated
				idx.Bleve.Delete(scene.SceneID)
			}

			err := idx.PutScene(scene)
			if err != nil {
				log.Error(err)
			} else {
				// log.Debugln("Indexed " + scene.SceneID)
				total += 1
			}
		}

		idx.Bleve.Close()

		tlog.Infof("Indexed %v scenes", total)
	}
}

func DeleteIndexScenes(scenes *[]models.Scene) {
	if !models.CheckLock("index") {
		models.CreateLock("index")
		defer models.RemoveLock("index")

		tlog := log.WithFields(logrus.Fields{"task": "scrape"})

		idx, err := NewIndex("scenes")
		if err != nil {
			log.Error(err)
			models.RemoveLock("index")
			return
		}

		tlog.Infof("Deleting scenes from search index...")

		total := 0
		lastMessage := time.Now()
		for i := range *scenes {
			if time.Since(lastMessage) > time.Duration(config.Config.Advanced.ProgressTimeInterval)*time.Second {
				tlog.Infof("Deleting scene index %v of %v scenes", total, len(*scenes))
				lastMessage = time.Now()
			}
			scene := (*scenes)[i]
			if idx.Exist(scene.SceneID) {
				// Remove old index, as data may have been updated
				idx.Bleve.Delete(scene.SceneID)
			}
		}

		idx.Bleve.Close()

		tlog.Infof("Indexed %v scenes", total)
	}
}

/**
 * Update search index for all of the specified scrapedScenes.
 * This method will first read the scraped scenes from the DB, after
 * which it calls IndexScenes.
 */
func IndexScrapedScenes(scrapedScenes *[]models.ScrapedScene) {
	// Map scrapedScenes to Scenes
	var scenes []models.Scene
	for i := range *scrapedScenes {
		var scene models.Scene
		scrapedScene := (*scrapedScenes)[i]
		// Read scraped scene from db, as we don't want to index it
		// if it doesn't exist in there
		err := scene.GetIfExist(scrapedScene.SceneID)
		if err == nil {
			scenes = append(scenes, scene)
		}
	}

	// Now update search index
	IndexScenes(&scenes)
}

func CleanFilename(filename string) string {
	commonWords := []string{
		"180", "180x180", "2880x1440", "3d", "3dh", "3dv", "30fps", "30m", "360",
		"3840x1920", "4k", "5k", "5400x2700", "60fps", "6k", "7k", "7680x3840",
		"8k", "fb360", "fisheye190", "funscript", "cmscript", "h264", "h265", "hevc", "hq", "hsp", "lq", "lr",
		"mkv", "mkx200", "mkx220", "mono", "mp4", "oculus", "oculus5k",
		"oculusrift", "original", "rf52", "smartphone", "srt", "ssa", "tb", "uhq", "vrca220", "vp9",
	}

	// Remove extension
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)

	// Replace characters with spaces
	re := regexp.MustCompile(`[._+-]`)
	name = re.ReplaceAllString(name, " ")

	// Replace multiple spaces
	re = regexp.MustCompile(`\s+`)
	name = re.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)

	// Filter common words
	parts := strings.Split(name, " ")
	var filtered []string
	resP := regexp.MustCompile(`^[0-9]+p$`)
	for _, p := range parts {
		isCommon := false
		for _, cw := range commonWords {
			if strings.EqualFold(p, cw) {
				isCommon = true
				break
			}
		}
		if !isCommon && !resP.MatchString(strings.ToLower(p)) {
			filtered = append(filtered, p)
		}
	}

	result := strings.Join(filtered, " ")
	result = strings.ReplaceAll(result, " s ", "'s ")

	// Detect JAVR-style patterns like "PXVR 258" or "SAVR 883" and add variations
	javrPattern := regexp.MustCompile(`([a-zA-Z]+)\s+([0-9]+)`)
	matches := javrPattern.FindAllStringSubmatch(result, -1)

	for _, match := range matches {
		if len(match) == 3 {
			prefix := match[1]
			numStr := match[2]

			// Add zero-padded version (e.g., PXVR00258)
			num, err := strconv.Atoi(numStr)
			if err == nil {
				padded := fmt.Sprintf("%s%05d", prefix, num)
				if !strings.Contains(result, padded) {
					result = result + " " + padded
				}
			}

			// Add simple concatenated version (e.g., PXVR258)
			simple := prefix + numStr
			if !strings.Contains(result, simple) {
				result = result + " " + simple
			}
		}
	}

	return result
}

func FuzzySearchScenes(q string) []models.Scene {
	db, _ := models.GetDB()
	defer db.Close()

	idx, err := NewIndex("scenes")
	if err != nil {
		return nil
	}
	defer idx.Bleve.Close()

	query := bleve.NewQueryStringQuery(q)
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Fields = []string{"Id", "title", "cast", "site", "description"}
	searchRequest.Size = 25
	searchRequest.SortBy([]string{"-_score"})

	searchResults, err := idx.Bleve.Search(searchRequest)
	if err != nil {
		return nil
	}

	var scenes []models.Scene
	for _, v := range searchResults.Hits {
		var scene models.Scene
		err := scene.GetIfExist(v.ID)
		if err != nil {
			continue
		}

		scene.Score = v.Score
		scenes = append(scenes, scene)
	}

	return scenes
}
