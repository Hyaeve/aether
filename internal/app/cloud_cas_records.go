package app

import "time"

type cloudCASRecord struct {
	ID        string    `json:"id"`
	StorageID string    `json:"storageId"`
	Parent    string    `json:"parent"`
	Time      time.Time `json:"time"`
	Status    string    `json:"status"`
	CAS       CASInfo   `json:"cas"`
	Revision  string    `json:"revision"`
}

func (a *App) beginCloudCASRecord(s Storage, parent string, info CASInfo) (string, error) {
	key := id()
	err := a.store.update(func(st *State) error {
		st.CloudCASRecords = append([]cloudCASRecord{{ID: key, StorageID: s.ID, Parent: parent, Time: time.Now(), Status: "running", CAS: info, Revision: rapid115Stamp(s.Config)}}, st.CloudCASRecords...)
		if len(st.CloudCASRecords) > 1000 {
			st.CloudCASRecords = st.CloudCASRecords[:1000]
		}
		return nil
	})
	return key, err
}
func (a *App) finishCloudCASRecord(key string, success bool) {
	_ = a.store.update(func(st *State) error {
		for i := range st.CloudCASRecords {
			if st.CloudCASRecords[i].ID == key {
				st.CloudCASRecords[i].Status = "error"
				if success {
					st.CloudCASRecords[i].Status = "success"
				}
				break
			}
		}
		return nil
	})
}
