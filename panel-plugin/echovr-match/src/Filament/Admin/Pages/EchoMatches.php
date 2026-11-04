<?php

namespace MarshmallowMia\EchoVrMatch\Filament\Admin\Pages;

use App\Models\Server;
use BackedEnum;
use Filament\Pages\Page;
use MarshmallowMia\EchoVrMatch\Filament\Server\Pages\MatchPage;
use MarshmallowMia\EchoVrMatch\Services\MatchReader;
use Throwable;

/** Every Echo VR server's match on one page. */
class EchoMatches extends Page
{
    protected static string|BackedEnum|null $navigationIcon = 'tabler-trophy';

    protected static ?int $navigationSort = 3;

    protected static ?string $slug = 'echo-matches';

    protected string $view = 'echovr-match::pages.admin-matches';

    public static function getNavigationLabel(): string
    {
        return trans('echovr-match::match.admin_nav');
    }

    public static function getNavigationGroup(): ?string
    {
        return trans('admin/dashboard.server');
    }

    public function getTitle(): string
    {
        return trans('echovr-match::match.admin_title');
    }

    public static function canAccess(): bool
    {
        return user()?->can('viewAny', Server::class) ?? false;
    }

    protected function getViewData(): array
    {
        $rows = [];
        $servers = Server::query()->with(['egg', 'node', 'allocation'])->orderBy('name')->get()
            ->filter(fn (Server $s) => MatchReader::isEcho($s));
        foreach ($servers as $server) {
            $url = null;
            try {
                $url = MatchPage::getUrl(panel: 'server', tenant: $server);
            } catch (Throwable) {
            }
            $rows[] = [
                'server' => $server,
                'match' => MatchReader::match($server),
                'status' => MatchReader::status($server)['data'],
                'url' => $url,
            ];
        }

        return ['rows' => $rows, 'poll' => config('echovr-match.poll.admin', '5s')];
    }
}
