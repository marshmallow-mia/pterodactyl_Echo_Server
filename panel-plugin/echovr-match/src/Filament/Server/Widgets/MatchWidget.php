<?php

namespace MarshmallowMia\EchoVrMatch\Filament\Server\Widgets;

use App\Enums\SubuserPermission;
use App\Models\Server;
use Filament\Facades\Filament;
use Filament\Notifications\Notification;
use Filament\Widgets\Widget;
use MarshmallowMia\EchoVrMatch\Filament\Server\Pages\MatchPage;
use MarshmallowMia\EchoVrMatch\Services\MatchReader;
use Throwable;

/** The live match above the console of an Echo VR server. */
class MatchWidget extends Widget
{
    protected string $view = 'echovr-match::widgets.match';

    protected int|string|array $columnSpan = 'full';

    protected static bool $isLazy = false;

    public ?Server $server = null;

    public static function canView(): bool
    {
        $server = Filament::getTenant();

        return $server instanceof Server && MatchReader::isEcho($server);
    }

    protected function getViewData(): array
    {
        $server = $this->server ?? Filament::getTenant();

        return [
            'match' => MatchReader::match($server),
            'status' => MatchReader::status($server)['data'],
            'apiOn' => MatchReader::apiSetting($server),
            'canToggle' => user()?->can(SubuserPermission::ControlConsole, $server) ?? false,
            'pageUrl' => MatchPage::getUrl(tenant: $server),
            'poll' => config('echovr-match.poll.widget', '3s'),
        ];
    }

    public function toggleApi(): void
    {
        $server = $this->server ?? Filament::getTenant();
        abort_unless(user()?->can(SubuserPermission::ControlConsole, $server), 403);
        $on = !MatchReader::apiSetting($server);
        try {
            $key = MatchReader::setApi($server, $on);
            Notification::make()->success()->title(trans($on ? 'echovr-match::match.api.turned_on' : 'echovr-match::match.api.turned_off'))->body(trans($key))->send();
        } catch (Throwable $e) {
            report($e);
            Notification::make()->danger()->title(trans('echovr-match::match.api.failed'))->body($e->getMessage())->send();
        }
    }
}
