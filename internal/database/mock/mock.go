package mock

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
)

// MockDB is a mock implementation of database.DB for testing.
type MockDB struct {
	mu sync.RWMutex

	// Media storage
	mediaItems       map[uint]*database.Media
	nextMediaID      uint
	deletedMediaTMDB map[int32][]database.Media
	deletedMediaTVDB map[int32][]database.Media

	// User storage
	users      map[uint]*database.User
	nextUserID uint

	// Request storage
	requests      map[uint]*database.Request
	nextRequestID uint

	// Error simulation
	CreateMediaItemsError           error
	GetMediaItemByIDError           error
	GetMediaItemsError              error
	GetMediaItemsByMediaTypeError   error
	GetMediaWithPendingRequestError error
	GetMediaExpiredProtectionError  error
	GetDeletedMediaByTMDBIDError    error
	GetDeletedMediaByTVDBIDError    error
	SetMediaProtectedUntilError     error
	MarkMediaAsUnkeepableError      error
	DeleteMediaItemError            error
	CreateUserError                 error
	GetUserByUsernameError          error
	GetUserByIDError                error
	CreateRequestError              error
	UpdateRequestStatusError        error
}

// NewMockDB creates a new MockDB instance.
func NewMockDB() *MockDB {
	return &MockDB{
		mediaItems:       make(map[uint]*database.Media),
		nextMediaID:      1,
		deletedMediaTMDB: make(map[int32][]database.Media),
		deletedMediaTVDB: make(map[int32][]database.Media),
		users:            make(map[uint]*database.User),
		nextUserID:       1,
		requests:         make(map[uint]*database.Request),
		nextRequestID:    1,
	}
}

// Reset clears all data and errors from the mock database.
func (m *MockDB) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mediaItems = make(map[uint]*database.Media)
	m.nextMediaID = 1
	m.deletedMediaTMDB = make(map[int32][]database.Media)
	m.deletedMediaTVDB = make(map[int32][]database.Media)
	m.users = make(map[uint]*database.User)
	m.nextUserID = 1
	m.requests = make(map[uint]*database.Request)
	m.nextRequestID = 1

	m.CreateMediaItemsError = nil
	m.GetMediaItemByIDError = nil
	m.GetMediaItemsError = nil
	m.GetMediaItemsByMediaTypeError = nil
	m.GetMediaWithPendingRequestError = nil
	m.GetMediaExpiredProtectionError = nil
	m.GetDeletedMediaByTMDBIDError = nil
	m.GetDeletedMediaByTVDBIDError = nil
	m.SetMediaProtectedUntilError = nil
	m.MarkMediaAsUnkeepableError = nil
	m.DeleteMediaItemError = nil
	m.CreateUserError = nil
	m.GetUserByUsernameError = nil
	m.GetUserByIDError = nil
	m.CreateRequestError = nil
	m.UpdateRequestStatusError = nil
}

// Media operations

func (m *MockDB) CreateMediaItems(ctx context.Context, items []database.Media) error {
	if m.CreateMediaItemsError != nil {
		return m.CreateMediaItemsError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range items {
		items[i].ID = m.nextMediaID
		m.nextMediaID++
		m.mediaItems[items[i].ID] = &items[i]
	}

	return nil
}

func (m *MockDB) GetMediaItemByID(ctx context.Context, id uint) (*database.Media, error) {
	if m.GetMediaItemByIDError != nil {
		return nil, m.GetMediaItemByIDError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	media, ok := m.mediaItems[id]
	if !ok {
		return nil, errors.New("media item not found")
	}

	return media, nil
}

func (m *MockDB) GetMediaItems(ctx context.Context, includeProtected bool) ([]database.Media, error) {
	if m.GetMediaItemsError != nil {
		return nil, m.GetMediaItemsError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []database.Media
	now := time.Now()

	for _, media := range m.mediaItems {
		if includeProtected {
			items = append(items, *media)
		} else {
			if media.ProtectedUntil == nil || media.ProtectedUntil.Before(now) {
				items = append(items, *media)
			}
		}
	}

	return items, nil
}

func (m *MockDB) GetMediaItemsByMediaType(ctx context.Context, mediaType database.MediaType) ([]database.Media, error) {
	if m.GetMediaItemsByMediaTypeError != nil {
		return nil, m.GetMediaItemsByMediaTypeError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []database.Media
	now := time.Now()

	for _, media := range m.mediaItems {
		if media.MediaType == mediaType {
			if media.ProtectedUntil == nil || media.ProtectedUntil.Before(now) {
				items = append(items, *media)
			}
		}
	}

	return items, nil
}

func (m *MockDB) GetMediaWithPendingRequest(ctx context.Context) ([]database.Media, error) {
	if m.GetMediaWithPendingRequestError != nil {
		return nil, m.GetMediaWithPendingRequestError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []database.Media
	now := time.Now()

	for _, media := range m.mediaItems {
		if media.ProtectedUntil == nil || media.ProtectedUntil.Before(now) {
			if req, ok := m.requests[media.ID]; ok && req.Status == database.RequestStatusPending {
				items = append(items, *media)
			}
		}
	}

	return items, nil
}

func (m *MockDB) GetMediaExpiredProtection(ctx context.Context, asOf time.Time) ([]database.Media, error) {
	if m.GetMediaExpiredProtectionError != nil {
		return nil, m.GetMediaExpiredProtectionError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []database.Media

	for _, media := range m.mediaItems {
		if media.ProtectedUntil != nil && !media.ProtectedUntil.After(asOf) {
			items = append(items, *media)
		}
	}

	return items, nil
}

func (m *MockDB) GetDeletedMediaByTMDBID(ctx context.Context, tmdbID int32) ([]database.Media, error) {
	if m.GetDeletedMediaByTMDBIDError != nil {
		return nil, m.GetDeletedMediaByTMDBIDError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	items, ok := m.deletedMediaTMDB[tmdbID]
	if !ok {
		return []database.Media{}, nil
	}

	return items, nil
}

func (m *MockDB) GetDeletedMediaByTVDBID(ctx context.Context, tvdbID int32) ([]database.Media, error) {
	if m.GetDeletedMediaByTVDBIDError != nil {
		return nil, m.GetDeletedMediaByTVDBIDError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	items, ok := m.deletedMediaTVDB[tvdbID]
	if !ok {
		return []database.Media{}, nil
	}

	return items, nil
}

func (m *MockDB) SetMediaProtectedUntil(ctx context.Context, mediaID uint, protectedUntil *time.Time) error {
	if m.SetMediaProtectedUntilError != nil {
		return m.SetMediaProtectedUntilError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	media, ok := m.mediaItems[mediaID]
	if !ok {
		return errors.New("media item not found")
	}

	media.ProtectedUntil = protectedUntil
	media.Unkeepable = false

	return nil
}

func (m *MockDB) MarkMediaAsUnkeepable(ctx context.Context, mediaID uint) error {
	if m.MarkMediaAsUnkeepableError != nil {
		return m.MarkMediaAsUnkeepableError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	media, ok := m.mediaItems[mediaID]
	if !ok {
		return errors.New("media item not found")
	}

	media.Unkeepable = true
	media.ProtectedUntil = nil

	return nil
}

func (m *MockDB) DeleteMediaItem(ctx context.Context, media *database.Media) error {
	if m.DeleteMediaItemError != nil {
		return m.DeleteMediaItemError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existingMedia, ok := m.mediaItems[media.ID]
	if !ok {
		return errors.New("media item not found")
	}

	// Update the delete reason
	existingMedia.DBDeleteReason = media.DBDeleteReason

	// Store in deleted media maps
	if existingMedia.TmdbId != nil {
		m.deletedMediaTMDB[*existingMedia.TmdbId] = append(m.deletedMediaTMDB[*existingMedia.TmdbId], *existingMedia)
	}
	if existingMedia.TvdbId != nil {
		m.deletedMediaTVDB[*existingMedia.TvdbId] = append(m.deletedMediaTVDB[*existingMedia.TvdbId], *existingMedia)
	}

	// Remove from active media
	delete(m.mediaItems, media.ID)

	return nil
}

// User operations

func (m *MockDB) CreateUser(ctx context.Context, username string) (*database.User, error) {
	if m.CreateUserError != nil {
		return nil, m.CreateUserError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	user := &database.User{
		Username: username,
	}
	user.ID = m.nextUserID
	m.nextUserID++

	m.users[user.ID] = user

	return user, nil
}

func (m *MockDB) GetUserByUsername(ctx context.Context, username string) (*database.User, error) {
	if m.GetUserByUsernameError != nil {
		return nil, m.GetUserByUsernameError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, user := range m.users {
		if user.Username == username {
			return user, nil
		}
	}

	return nil, errors.New("user not found")
}

func (m *MockDB) GetUserByID(ctx context.Context, id uint) (*database.User, error) {
	if m.GetUserByIDError != nil {
		return nil, m.GetUserByIDError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	user, ok := m.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}

	return user, nil
}

func (m *MockDB) GetOrCreateUser(ctx context.Context, username string) (*database.User, error) {
	user, err := m.GetUserByUsername(ctx, username)
	if err != nil {
		return m.CreateUser(ctx, username)
	}
	return user, nil
}

// Request operations

func (m *MockDB) CreateRequest(ctx context.Context, mediaID uint, userID uint) (*database.Request, error) {
	if m.CreateRequestError != nil {
		return nil, m.CreateRequestError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	request := &database.Request{
		MediaID: mediaID,
		UserID:  userID,
		Status:  database.RequestStatusPending,
	}
	request.ID = m.nextRequestID
	m.nextRequestID++

	m.requests[mediaID] = request

	return request, nil
}

func (m *MockDB) UpdateRequestStatus(ctx context.Context, requestID uint, status database.RequestStatus) error {
	if m.UpdateRequestStatusError != nil {
		return m.UpdateRequestStatusError
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, request := range m.requests {
		if request.ID == requestID {
			request.Status = status
			return nil
		}
	}

	return errors.New("request not found")
}

// Helper methods for testing

// AddDeletedMedia adds a media item to the deleted media history for testing.
// The deletedAt parameter sets when the media was deleted.
func (m *MockDB) AddDeletedMedia(media database.Media, deletedAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if media.TmdbId != nil {
		m.deletedMediaTMDB[*media.TmdbId] = append(m.deletedMediaTMDB[*media.TmdbId], media)
	}
	if media.TvdbId != nil {
		m.deletedMediaTVDB[*media.TvdbId] = append(m.deletedMediaTVDB[*media.TvdbId], media)
	}
}
