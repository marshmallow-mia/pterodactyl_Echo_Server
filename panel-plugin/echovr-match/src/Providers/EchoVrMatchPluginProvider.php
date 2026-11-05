<?php

namespace MarshmallowMia\EchoVrMatch\Providers;

use App\Enums\ConsoleWidgetPosition;
use App\Filament\Server\Pages\Console;
use App\Models\Server;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\ServiceProvider;
use MarshmallowMia\EchoVrMatch\Filament\Server\Widgets\MatchWidget;
use MarshmallowMia\EchoVrMatch\Services\Format;
use MarshmallowMia\EchoVrMatch\Services\MatchReader;

class EchoVrMatchPluginProvider extends ServiceProvider
{
    public function register(): void
    {
        Console::registerCustomWidgets(ConsoleWidgetPosition::AboveConsole, [MatchWidget::class]);
    }

    public function boot(): void
    {
        // The live map's level drawings, for signed-in users; the browser keeps
        // them (the URL carries the file's hash), so the Match page's polls
        // only send the dots.
        if ($this->app->routesAreCached()) {
            return;
        }
        Route::middleware(['web', 'auth'])->get('echovr-match/maps/{map}.svg', function (string $map) {
            $path = Format::mapFile($map);
            abort_if($path === null, 404);

            return response()->file($path, [
                'Content-Type' => 'image/svg+xml',
                'Cache-Control' => 'max-age=31536000, immutable',
                'X-Content-Type-Options' => 'nosniff',
            ]);
        })->where('map', '[a-z0-9_]+')->name('echovr-match.map');

        // The live map's positions, for anyone who may open the server (as
        // its Match page): fetched several times a second while it is open.
        Route::middleware(['web', 'auth'])->get('echovr-match/servers/{server:uuid}/live.json', function (Request $request, Server $server) {
            abort_unless($request->user()?->canAccessTenant($server) && MatchReader::isEcho($server), 404);
            $live = MatchReader::live($server);

            return $live === null
                ? response()->noContent()->header('Cache-Control', 'no-store')
                : response()->json($live)->header('Cache-Control', 'no-store');
        })->name('echovr-match.live');
    }
}
