package teamcity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type TeamCity struct {
	URL         string
	AccessToken string
}

type Artifacts struct {
	Files []struct {
		Name string `json:"name"`
		Href string `json:"href"`
	} `json:"file"`
}

func (tc *TeamCity) FindArtifact(buildId int, filename string) (string, error) {
	metadataPrefix := fmt.Sprintf("/app/rest/builds/id:%d/artifacts/metadata/", buildId)
	queue := []string{""}

	for {
		parentPath := queue[0]
		queue = queue[1:]
		url := fmt.Sprintf("%s/app/rest/builds/id:%d/artifacts", tc.URL, buildId)
		if parentPath != "" {
			url += "/children/" + parentPath
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer " + tc.AccessToken)
		req.Header.Set("Accept", "application/json")
	
		client := http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		if resp.StatusCode == http.StatusNotFound {
			if parentPath == "" {
				return "", fmt.Errorf("build %v has no artifacts", buildId)
			}
			// Else continue with the next path
		} else if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("error reading artifacts: %v", string(buf))
		}

		var arts Artifacts
		err = json.Unmarshal(buf, &arts)
		if err != nil {
			return "", err
		}
		for _, file := range(arts.Files) {
			// Strip off the prefix to get the file path
			path := file.Href[len(metadataPrefix):]
			if file.Name == filename {
				return path, nil
			} else {
				queue = append(queue, path)
			}
		}
		if len(queue) == 0 {
			break
		}
	}

	return "", fmt.Errorf("no such artifact %v", filename)
}

func (tc *TeamCity) DownloadHref(href string) ([]byte, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", tc.URL, href), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer " + tc.AccessToken)

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no such object %v", href)
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download error: %v", string(buf))
	}
	return buf, nil
}

func (tc *TeamCity) DownloadArtifact(buildId int, path string) ([]byte, error) {
	return tc.DownloadHref(fmt.Sprintf("/app/rest/builds/id:%d/artifacts/content/%s", buildId, path))
}
