package app

import (
	"context"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
)

// resetAccount keeps process preferences and services, and discards everything
// loaded or navigated in the previous account. Cancelling the account also
// fences updates already queued before sign-out.
func resetAccount(s *State) {
	oldAccount := s.username
	cache := s.cache
	if cache != nil && oldAccount != "" {
		cleanup := cache.PreparePurge(oldAccount)
		flatte.Go(s.fx, "library-cache-purge", func(context.Context) (struct{}, error) {
			return struct{}{}, cleanup()
		}, func(s *State, _ struct{}, err error) {
			if err != nil {
				s.logf("library cache purge: %v", err)
			}
		})
	}
	if s.playerEvents != nil {
		s.playerEvents.Cancel()
	}
	if s.account != nil {
		s.account.Cancel()
	}
	for _, name := range []string{"home", "search", "volume", "seek", "change-audio", "rename-device"} {
		flatte.Cancel(s.fx, name)
	}
	if web, ok := s.web.(interface{ Reset() }); ok {
		web.Reset()
	}
	fresh := New(s.be, s.img, s.web, s.debug).State
	fresh.cache = cache
	fresh.fx, fresh.windowed, fresh.away = s.fx, s.windowed, s.away
	fresh.playerEventsDone = s.playerEventsDone
	fresh.settings, fresh.settingsPath, fresh.settingsWriter = s.settings, s.settingsPath, s.settingsWriter
	fresh.activeAudio = s.activeAudio
	fresh.compact, fresh.searchDelay = s.compact, s.searchDelay
	fresh.deviceName = flatui.TextField{Value: s.settings.DeviceName, Cursor: len(s.settings.DeviceName)}
	*s = *fresh
}

func accountScope(s *State) *flatte.Scope {
	if s.account == nil {
		s.account = flatte.NewScope(s.fx, "account")
	}
	return s.account
}

// accountContext preserves both request cancellation (Latest) and the
// account lifetime. Work and queued folds from a signed-out account stop.
func accountContext(ctx context.Context, scope *flatte.Scope) (context.Context, func()) {
	combined, cancel := context.WithCancel(scope.Ctx())
	stop := context.AfterFunc(ctx, cancel)
	if ctx.Err() != nil {
		cancel()
	}
	return combined, func() { stop(); cancel() }
}

func accountGo[T any](s *State, name string, work func(context.Context) (T, error), fold func(*State, T, error)) {
	scope := accountScope(s)
	flatte.Go(s.fx, name, func(ctx context.Context) (T, error) {
		ctx, stop := accountContext(ctx, scope)
		defer stop()
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, err
		}
		return work(ctx)
	}, func(s *State, value T, err error) {
		if scope.Ctx().Err() == nil {
			fold(s, value, err)
		}
	})
}

func accountLatest[T any](s *State, name string, work func(context.Context) (T, error), fold func(*State, T, error)) {
	scope := accountScope(s)
	flatte.Latest(s.fx, name, func(ctx context.Context) (T, error) {
		ctx, stop := accountContext(ctx, scope)
		defer stop()
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, err
		}
		return work(ctx)
	}, func(s *State, value T, err error) {
		if scope.Ctx().Err() == nil {
			fold(s, value, err)
		}
	})
}
