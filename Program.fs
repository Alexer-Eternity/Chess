open System
open System.IO
open Microsoft.AspNetCore.Builder
open Microsoft.Extensions.Hosting
open Microsoft.AspNetCore.Hosting
open Microsoft.Extensions.DependencyInjection
open Giraffe
open ChessDotNet
open ChessDotNet.Pieces
open MongoDB.Driver
open MongoDB.Bson.Serialization.Attributes

// 1. DOMAIN
[<CLIMutable>]
type MoveRequest = { 
    GameId: string     
    PlayerId: string  
    Move: string 
}

[<CLIMutable>]
type JoinRequest = {
    GameId: string
    PlayerId: string
    Color: string      
}

type GameState = {
    [<BsonId>] GameId: string
    WhitePlayerId: string
    BlackPlayerId: string
    Fen: string
    [<BsonDefaultValue("")>]
    LastMove: string // Optional: useful for history
    [<BsonDefaultValue("")>]
    Result: string
}
let startFen = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
//2 Helpers

let isPlayerTurn (fen: string) (game: GameState) (playerId: string) =
    if fen.Contains(" w ") then
        game.WhitePlayerId = playerId 
        

    else
        game.BlackPlayerId = playerId // It's Black's turn, is this player Black?
let calculateNewFen (currentFen: string) (moveString: string) :Result<(string * string), string> =
    try

        // 1. Create Game
        let game = ChessGame(currentFen)

        // 2. Parse Coordinates
        if moveString.Length < 4 then 
            Error "Move too short"
        else
            let src = moveString.Substring(0, 2) // e.g. "e2"
            let dst = moveString.Substring(2, 2) // e.g. "e4"
            
            // Handle Promotion (default to Queen if 'q' is present)
            let promotion : Nullable<char> = 
                if moveString.Length > 4 && Char.ToUpper(moveString.[4]) = 'Q' then 
                    Nullable('Q') 
                else 
                    Nullable()

            // 3. Construct Move
            let move = Move(src, dst, game.WhoseTurn, promotion)

            // 4. Execute
            let status = game.MakeMove(move, false)

            if status = MoveType.Invalid then
                Error $"Invalid Move: {moveString}"
            else
                
                let opponent = game.WhoseTurn
                let gameResult =
                    if game.IsCheckmated(opponent) then
                        if opponent = Player.White then "Black Wins" else "White Wins"
                    elif game.IsStalemated(opponent) || game.IsInsufficientMaterial() then
                        "Draw"
                    else
                        ""
                Ok (game.GetFen(), gameResult)

    with ex ->
        printfn "DEBUG: CRASH: %s" ex.Message
        Error $"Chess Error: {ex.Message}"




let handleJoin (games: IMongoCollection<GameState>) : HttpHandler =
    fun next ctx -> task {
        let! req = ctx.BindJsonAsync<JoinRequest>()
        let gameId = req.GameId
        
        let! existingGame = games.Find(Builders<GameState>.Filter.Eq("_id", gameId)).FirstOrDefaultAsync()

        if isNull (box existingGame) then
            let newGame = 
                match req.Color.ToLower() with
                | "white" -> { GameId = gameId; WhitePlayerId = req.PlayerId; BlackPlayerId = ""; Fen = startFen; LastMove = "";Result ="" }
                | "black" -> { GameId = gameId; WhitePlayerId = ""; BlackPlayerId = req.PlayerId; Fen = startFen; LastMove = "" ;Result =""}
                | _ -> { GameId = gameId; WhitePlayerId = ""; BlackPlayerId = ""; Fen = startFen; LastMove = "" ;Result =""}
            
            do! games.InsertOneAsync(newGame)
            return! json {| Success = true; Message = $"Created room and joined as {req.Color}"; Fen = startFen |} next ctx
        elif existingGame.Result <> "" then
            //delete the old finished game
            let! _ = games.DeleteOneAsync(Builders<GameState>.Filter.Eq("_id", gameId))
            do! System.Threading.Tasks.Task.Delay(1000)
            let newGame = 
                match req.Color.ToLower() with
                // FIX 1: Added 'LastMove = ""' to all these records
                | "white" -> { GameId = gameId; WhitePlayerId = req.PlayerId; BlackPlayerId = ""; Fen = startFen; LastMove = "";Result ="" }
                | "black" -> { GameId = gameId; WhitePlayerId = ""; BlackPlayerId = req.PlayerId; Fen = startFen; LastMove = "" ;Result =""}
                | _ -> { GameId = gameId; WhitePlayerId = ""; BlackPlayerId = ""; Fen = startFen; LastMove = "" ;Result =""}
            
            do! games.InsertOneAsync(newGame)
            return! json {| Success = true; Message = "Game was over. Room Reset!"; Fen = newGame.Fen |} next ctx
        else
            // CASE B: Game exists - check seat
            let isWhiteRequest = req.Color.ToLower() = "white"
            let currentOwner = if isWhiteRequest then existingGame.WhitePlayerId else existingGame.BlackPlayerId
            
            let isSeatFree = System.String.IsNullOrEmpty(currentOwner)
            let isMySeat = currentOwner = req.PlayerId

            if isSeatFree || isMySeat then
                let fieldName = if isWhiteRequest then "WhitePlayerId" else "BlackPlayerId"
                let update = Builders<GameState>.Update.Set(fieldName, req.PlayerId)
                
                let! _ = games.UpdateOneAsync(Builders<GameState>.Filter.Eq("_id", gameId), update)
                
                return! json {| Success = true; Message = $"Joined as {req.Color}"; Fen = existingGame.Fen |} next ctx
            else
                return! json {| Success = false; Message = $"Error: The {req.Color} seat is already taken by '{currentOwner}'!" |} next ctx
    }
let handleMove (games: IMongoCollection<GameState>) : HttpHandler =
    fun next ctx -> task {
        let! req = ctx.BindJsonAsync<MoveRequest>()

        // 1. Load Game
        let! game = games.Find(Builders<GameState>.Filter.Eq("GameId", req.GameId)).FirstOrDefaultAsync()
        
        if isNull (box game) then
            return! json {| Success = false; Message = "Game not found" |} next ctx
        elif game.Result <> "" then
            return! json {| Success = false; Message = $"Game Over: {game.Result}" |} next ctx
        else
            let isWhitePlayer = (req.PlayerId = game.WhitePlayerId)
            let isBlackPlayer = (req.PlayerId = game.BlackPlayerId)

            if not (isWhitePlayer || isBlackPlayer) then
                return! json {| Success = false; Message = "Spectators cannot interact." |} next ctx
            else
                let command = req.Move.Trim().ToLower()

                if command = "resign" then
                    let result = if isWhitePlayer then "Black Wins" else "White Wins"
                    
                    let update = Builders<GameState>.Update.Set("Result", result)
                    let! _ = games.UpdateOneAsync(Builders<GameState>.Filter.Eq("_id", req.GameId), update)
                    
                    return! json {| Success = true; Message = $"GAME OVER: {result}"; Fen = game.Fen; Result = result |} next ctx

                elif command = "draw" then
                    let myDrawToken = if isWhitePlayer then "WhiteDraw" else "BlackDraw"
                    let opponentDrawToken = if isWhitePlayer then "BlackDraw" else "WhiteDraw"

                    if game.LastMove = opponentDrawToken then
                        let result = "Draw"
                        let update = Builders<GameState>.Update.Set("Result", result)
                        let! _ = games.UpdateOneAsync(Builders<GameState>.Filter.Eq("_id", req.GameId), update)
                            
                        return! json {| Success = true; Message = "Draw Accepted! Game Over."; Fen = game.Fen; Result = "Draw" |} next ctx
                        
                    else

                        let update = Builders<GameState>.Update.Set("LastMove", myDrawToken)
                        let! _ = games.UpdateOneAsync(Builders<GameState>.Filter.Eq("_id", req.GameId), update)
                            
                        return! json {| Success = true; Message = $"Draw Offer Sent by {req.PlayerId}."; Fen = game.Fen |} next ctx    
                else
                    if not (isPlayerTurn game.Fen game req.PlayerId) then
                        return! json {| Success = false; Message = "Not your turn!" |} next ctx
                    else 
                        // 3. Apply Move 
                        match calculateNewFen game.Fen req.Move with
                        | Error err ->
                            return! json {| Success = false; Message = err |} next ctx
                        | Ok (newFen, gameResult) ->


                        // 4. Save
                            let update = Builders<GameState>.Update
                                                .Set("Fen", newFen)
                                                .Set("Result", gameResult)
                                                .Set("LastMove", req.Move)
                            let! res = games.UpdateOneAsync(Builders<GameState>.Filter.Eq("_id", req.GameId), update)
                            
                            printfn "💾 DB UPDATE: Matched %d, Modified %d" res.MatchedCount res.ModifiedCount
                            let message = 
                                if gameResult <> "" then $"GAME OVER: {gameResult}"
                                else "Move Valid"
                            return! json {| Success = true; Message = message; Fen = newFen; Result = gameResult |} next ctx
}
let handleGetGame (games: IMongoCollection<GameState>) (gameId: string) : HttpHandler =
    fun next ctx -> task {
        let! game = games.Find(Builders<GameState>.Filter.Eq("_id", gameId)).FirstOrDefaultAsync()

        if isNull (box game) then
            ctx.SetStatusCode 404
            return! json {| Message = "Game not found" |} next ctx
        else
            // Calculate whose turn it is based on the FEN string
            let fenParts = game.Fen.Split(' ')
            let turnColor = if fenParts.Length > 1 && fenParts.[1] = "b" then "Black" else "White"
            let message = 
                if game.Result <> "" then
                    $"Game Over: {game.Result}"
                else
                    // Check for Draw Offers stored in LastMove
                    match game.LastMove with
                    | "WhiteDraw" -> 
                        $"It is {turnColor}'s turn. WHITE offers a draw!"
                    | "BlackDraw" -> 
                        $"It is {turnColor}'s turn. BLACK offers a draw!"
                    | "" -> 
                        $"It is {turnColor}'s turn. (Start of Game)"
                    | move -> 
                        // Standard case: Show turn and the coordinate of the last move
                        $"It is {turnColor}'s turn. Last Move: {move}"
            return! json {| 
                Success = true
                Fen = game.Fen
                Message = message
                result=game.Result
            |} next ctx
    }


[<EntryPoint>]
let main args =
    let options = WebApplicationOptions(Args = args, WebRootPath = "WebRoot")
    let builder = WebApplication.CreateBuilder(options)
    builder.Services.AddGiraffe() |> ignore
    builder.Host.UseContentRoot(Directory.GetCurrentDirectory()) |> ignore

    // --- MONGO SETUP ---
    let DBString= System.Environment.GetEnvironmentVariable("MONGODB_CONNECTION_STRING")
    let mongoClient = MongoClient(DBString)
    let database = mongoClient.GetDatabase("ChessApp")
    let gamesCollection = database.GetCollection<GameState>("Games")

    let app = builder.Build()
    app.UseStaticFiles() |> ignore
    // --- ROUTING ---
    let webApp =
        choose [
            route "/"           >=> htmlFile "index.html"
            route "/login"      >=> htmlFile "login.html"
            route "/api/join"   >=> POST >=> handleJoin gamesCollection
            route "/api/move"   >=> POST >=> handleMove gamesCollection
            GET >=> routef "/api/game/%s" (handleGetGame gamesCollection)
        ]

    app.UseGiraffe webApp
    app.Run()
    0
    