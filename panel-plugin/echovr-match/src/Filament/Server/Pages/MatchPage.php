<?php

namespace MarshmallowMia\EchoVrMatch\Filament\Server\Pages;

use App\Enums\SubuserPermission;
use App\Models\Server;
use BackedEnum;
use Filament\Actions\Action;
use Filament\Facades\Filament;
use Filament\Notifications\Notification;
use Filament\Pages\Page;
use MarshmallowMia\EchoVrMatch\Services\Format;
use MarshmallowMia\EchoVrMatch\Services\MatchReader;
use Throwable;

/** A server's match: scoreboard, events and finished matches. */
class MatchPage extends Page
{
    protected static string|BackedEnum|null $navigationIcon = 'tabler-trophy';

    protected static ?int $navigationSort = 1;

    protected static ?string $slug = 'match';

    protected string $view = 'echovr-match::pages.match';

    public static function getNavigationLabel(): string
    {
        return trans('echovr-match::match.nav');
    }

    public function getTitle(): string
    {
        return trans('echovr-match::match.title');
    }

    public static function canAccess(): bool
    {
        return parent::canAccess() && MatchReader::isEcho(Filament::getTenant());
    }

    protected function server(): Server
    {
        /** @var Server $server */
        $server = Filament::getTenant();

        return $server;
    }

    protected function getHeaderActions(): array
    {
        return [
            Action::make('session_api')
                ->label(fn () => trans(MatchReader::apiSetting($this->server()) ? 'echovr-match::match.api.turn_off' : 'echovr-match::match.api.turn_on'))
                ->icon(fn () => MatchReader::apiSetting($this->server()) ? 'tabler-chart-bar-off' : 'tabler-chart-bar')
                ->color(fn () => MatchReader::apiSetting($this->server()) ? 'gray' : 'primary')
                ->authorize(fn () => user()?->can(SubuserPermission::ControlConsole, $this->server()))
                ->requiresConfirmation()
                ->modalDescription(trans('echovr-match::match.api.confirm'))
                ->action(function () {
                    $on = !MatchReader::apiSetting($this->server());
                    try {
                        $key = MatchReader::setApi($this->server(), $on);
                        Notification::make()->success()->title(trans($on ? 'echovr-match::match.api.turned_on' : 'echovr-match::match.api.turned_off'))->body(trans($key))->send();
                    } catch (Throwable $e) {
                        report($e);
                        Notification::make()->danger()->title(trans('echovr-match::match.api.failed'))->body($e->getMessage())->send();
                    }
                }),
            Action::make('telemetry')
                ->label(fn () => trans(MatchReader::telemetrySetting($this->server()) ? 'echovr-match::match.telemetry.turn_off' : 'echovr-match::match.telemetry.turn_on'))
                ->icon('tabler-activity')
                ->color('gray')
                ->authorize(fn () => user()?->can(SubuserPermission::ControlConsole, $this->server()))
                ->requiresConfirmation()
                ->modalDescription(trans('echovr-match::match.telemetry.confirm'))
                ->action(function () {
                    $on = !MatchReader::telemetrySetting($this->server());
                    try {
                        $key = MatchReader::setTelemetry($this->server(), $on);
                        Notification::make()->success()->title(trans($on ? 'echovr-match::match.telemetry.turned_on' : 'echovr-match::match.telemetry.turned_off'))->body(trans($key))->send();
                    } catch (Throwable $e) {
                        report($e);
                        Notification::make()->danger()->title(trans('echovr-match::match.telemetry.failed'))->body($e->getMessage())->send();
                    }
                }),
        ];
    }

    protected function getViewData(): array
    {
        $server = $this->server();
        $match = MatchReader::match($server);

        return [
            'match' => $match,
            'status' => MatchReader::status($server)['data'],
            'history' => MatchReader::history($server),
            'apiOn' => MatchReader::apiSetting($server),
            'poll' => config('echovr-match.poll.page', '3s'),
            'live' => [
                'url' => \Illuminate\Support\Facades\Route::has('echovr-match.live') ? route('echovr-match.live', ['server' => $server->uuid]) : null,
                'ms' => (int) config('echovr-match.live_ms', 200),
                // A lobby's positions come only from nEVR's telemetry: its map
                // shows while the server writes them (a runtime from
                // nevr-runtime #2306 on, telemetry on).
                'now' => Format::isLobby($match['data'] ?? null) ? MatchReader::live($server) !== null : true,
            ],
        ];
    }
}
