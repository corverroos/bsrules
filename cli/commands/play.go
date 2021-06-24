package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/corverroos/bsrules"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"
)

type Battlesnake struct {
	URL       string
	Name      string
	ID        string
	API       string
	LastMove  string
	Squad     string
	Character rune
}

type Coord struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

type SnakeResponse struct {
	Id      string  `json:"id"`
	Name    string  `json:"name"`
	Health  int32   `json:"health"`
	Body    []Coord `json:"body"`
	Latency string  `json:"latency"`
	Head    Coord   `json:"head"`
	Length  int32   `json:"length"`
	Shout   string  `json:"shout"`
	Squad   string  `json:"squad"`
}

type BoardResponse struct {
	Height  int32           `json:"height"`
	Width   int32           `json:"width"`
	Food    []Coord         `json:"food"`
	Hazards []Coord         `json:"hazards"`
	Snakes  []SnakeResponse `json:"snakes"`
}

type GameResponse struct {
	Id      string `json:"id"`
	Timeout int32  `json:"timeout"`
}

type ResponsePayload struct {
	Game  GameResponse  `json:"game"`
	Turn  int32         `json:"turn"`
	Board BoardResponse `json:"board"`
	You   SnakeResponse `json:"you"`
}

type PlayerResponse struct {
	Move  string `json:"move"`
	Shout string `json:"shout"`
}

type PingResponse struct {
	APIVersion string `json:"apiversion"`
	Author     string `json:"author"`
	Color      string `json:"color"`
	Head       string `json:"head"`
	Tail       string `json:"tail"`
	Version    string `json:"version"`
}

type Options struct {
	GameId string
	Turn int32
	Battlesnakes map[string]Battlesnake
	HttpClient http.Client
	Width int32
	Height int32
	Names []string
	URLs []string
	Squads []string
	Timeout int32
	Sequential bool
	GameType string
	ViewMap bool
	Seed int64
}

type Result struct {
	Turn int32
	Winner string
	Board *rules.BoardState
}

var playCmd = &cobra.Command{
	Use:   "play",
	Short: "Play a game of Battlesnake locally.",
	Long:  "Play a game of Battlesnake locally.",
}

func init() {
	rootCmd.AddCommand(playCmd)


	var o Options

	playCmd.Flags().Int32VarP(&o.Width, "width", "W", 11, "Width of Board")
	playCmd.Flags().Int32VarP(&o.Height, "height", "H", 11, "Height of Board")
	playCmd.Flags().StringArrayVarP(&o.Names, "name", "n", nil, "Name of Snake")
	playCmd.Flags().StringArrayVarP(&o.URLs, "url", "u", nil, "URL of Snake")
	playCmd.Flags().StringArrayVarP(&o.Names, "squad", "S", nil, "Squad of Snake")
	playCmd.Flags().Int32VarP(&o.Timeout, "timeout", "t", 500, "Request Timeout")
	playCmd.Flags().BoolVarP(&o.Sequential, "sequential", "s", false, "Use Sequential Processing")
	playCmd.Flags().StringVarP(&o.GameType, "gametype", "g", "standard", "Type of Game Rules")
	playCmd.Flags().BoolVarP(&o.ViewMap, "viewmap", "v", false, "View the Map Each Turn")
	playCmd.Flags().Int64VarP(&o.Seed, "seed", "r", time.Now().UTC().UnixNano(), "Random Seed")

	playCmd.Run = makeRun(&o)
}

var makeRun = func(o *Options) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		Run(o)
	}
}

func Run(o *Options) Result {
	rand.Seed(o.Seed)

	o.Battlesnakes = make(map[string]Battlesnake)
	o.GameId = uuid.New().String()
	o.Turn = 0

	snakes := buildSnakesFromOptions(o)

	var ruleset rules.Ruleset
	var royale rules.RoyaleRuleset
	var outOfBounds []rules.Point
	ruleset, _ = getRuleset(o, snakes)
	state := initializeBoardFromArgs(o, ruleset, snakes)
	for _, snake := range snakes {
		o.Battlesnakes[snake.ID] = snake
	}

	for v := false; !v; v, _ = ruleset.IsGameOver(state) {
		o.Turn++
		ruleset, royale = getRuleset(o, snakes)
		state, outOfBounds = createNextBoardState(o,ruleset, royale, state, outOfBounds, snakes)
		if o.ViewMap {
			printMap(o, state, outOfBounds)
		} else {
			log.Printf("[%v]: State: %v OutOfBounds: %v\n", o.Turn, state, outOfBounds)
		}
	}

	res := Result{
		Board: state,
		Turn: o.Turn,
	}

	if o.GameType == "solo" {
		log.Printf("[DONE]: Game completed after %v turns.", o.Turn)
	} else {
		var winner string
		isDraw := true
		for _, snake := range state.Snakes {
			if snake.EliminatedCause == rules.NotEliminated {
				isDraw = false
				winner = o.Battlesnakes[snake.ID].Name
				sendEndRequest(o, state, o.Battlesnakes[snake.ID])
			}
		}

		res.Winner = winner

		if isDraw {
			log.Printf("[DONE]: Game completed after %v turns. It was a draw.", o.Turn)
		} else {
			log.Printf("[DONE]: Game completed after %v turns. %v is the winner.", o.Turn, winner)
		}
	}

	return res
}

func getRuleset(o *Options, snakes []Battlesnake) (rules.Ruleset, rules.RoyaleRuleset) {
	var ruleset rules.Ruleset
	var royale rules.RoyaleRuleset

	standard := rules.StandardRuleset{
		FoodSpawnChance: 15,
		MinimumFood:     1,
	}

	switch o.GameType {
	case "royale":
		royale = rules.RoyaleRuleset{
			StandardRuleset:   standard,
			Seed:              o.Seed,
			Turn:              o.Turn,
			ShrinkEveryNTurns: 20,
			DamagePerTurn:     15,
		}
		ruleset = &royale
	case "squad":
		squadMap := map[string]string{}
		for _, snake := range snakes {
			squadMap[snake.ID] = snake.Squad
		}
		ruleset = &rules.SquadRuleset{
			StandardRuleset:     standard,
			SquadMap:            squadMap,
			AllowBodyCollisions: true,
			SharedElimination:   true,
			SharedHealth:        true,
			SharedLength:        true,
		}
	case "solo":
		ruleset = &rules.SoloRuleset{
			StandardRuleset: standard,
		}
	case "constrictor":
		ruleset = &rules.ConstrictorRuleset{
			StandardRuleset: standard,
		}
	default:
		ruleset = &standard
	}
	return ruleset, royale
}

func initializeBoardFromArgs(o *Options, ruleset rules.Ruleset, snakes []Battlesnake) *rules.BoardState {
	if o.Timeout == 0 {
		o.Timeout = 500
	}
	o.HttpClient = http.Client{
		Timeout: time.Duration(o.Timeout) * time.Millisecond,
	}

	snakeIds := []string{}
	for _, snake := range snakes {
		snakeIds = append(snakeIds, snake.ID)
	}
	state, err := ruleset.CreateInitialBoardState(o.Width, o.Height, snakeIds)
	if err != nil {
		log.Panic("[PANIC]: Error Initializing Board State")
		panic(err)
	}
	for _, snake := range snakes {
		requestBody := getIndividualBoardStateForSnake(o, state, snake, nil)
		u, _ := url.ParseRequestURI(snake.URL)
		u.Path = path.Join(u.Path, "start")
		_, err = o.HttpClient.Post(u.String(), "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			log.Printf("[WARN]: Request to %v failed", u.String())
		}
	}
	return state
}

func createNextBoardState(o *Options, ruleset rules.Ruleset, royale rules.RoyaleRuleset, state *rules.BoardState, outOfBounds []rules.Point, snakes []Battlesnake) (*rules.BoardState, []rules.Point) {
	var moves []rules.SnakeMove
	if o.Sequential {
		for _, snake := range snakes {
			moves = append(moves, getMoveForSnake(o, state, snake, outOfBounds))
		}
	} else {
		c := make(chan rules.SnakeMove, len(snakes))
		for _, snake := range snakes {
			go getConcurrentMoveForSnake(o, state, snake, outOfBounds, c)
		}
		for range snakes {
			moves = append(moves, <-c)
		}
	}
	for _, move := range moves {
		snake := o.Battlesnakes[move.ID]
		snake.LastMove = move.Move
		o.Battlesnakes[move.ID] = snake
	}
	if o.GameType == "royale" {
		_, err := royale.CreateNextBoardState(state, moves)
		if err != nil {
			log.Panic("[PANIC]: Error Producing Next Royale Board State")
			panic(err)
		}
	}
	state, err := ruleset.CreateNextBoardState(state, moves)
	if err != nil {
		log.Panic("[PANIC]: Error Producing Next Board State")
		panic(err)
	}
	return state, royale.OutOfBounds
}

func getConcurrentMoveForSnake(o *Options, state *rules.BoardState, snake Battlesnake, outOfBounds []rules.Point, c chan rules.SnakeMove) {
	c <- getMoveForSnake(o, state, snake, outOfBounds)
}

func getMoveForSnake(o *Options, state *rules.BoardState, snake Battlesnake, outOfBounds []rules.Point) rules.SnakeMove {
	requestBody := getIndividualBoardStateForSnake(o, state, snake, outOfBounds)
	u, _ := url.ParseRequestURI(snake.URL)
	u.Path = path.Join(u.Path, "move")
	res, err := o.HttpClient.Post(u.String(), "application/json", bytes.NewBuffer(requestBody))
	move := snake.LastMove
	if err != nil {
		log.Printf("[WARN]: Request to %v failed\n", u.String())
		log.Printf("Body --> %v\n", string(requestBody))
	} else if res.Body != nil {
		defer res.Body.Close()
		body, readErr := ioutil.ReadAll(res.Body)
		if readErr != nil {
			log.Fatal(readErr)
		} else {
			playerResponse := PlayerResponse{}
			jsonErr := json.Unmarshal(body, &playerResponse)
			if jsonErr != nil {
				log.Fatal(jsonErr)
			} else {
				move = playerResponse.Move
			}
		}
	}
	return rules.SnakeMove{ID: snake.ID, Move: move}
}

func sendEndRequest(o *Options, state *rules.BoardState, snake Battlesnake) {
	requestBody := getIndividualBoardStateForSnake(o, state, snake, nil)
	u, _ := url.ParseRequestURI(snake.URL)
	u.Path = path.Join(u.Path, "end")
	_, err := o.HttpClient.Post(u.String(), "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		log.Printf("[WARN]: Request to %v failed", u.String())
	}
}

func getIndividualBoardStateForSnake(o *Options, state *rules.BoardState, snake Battlesnake, outOfBounds []rules.Point) []byte {
	var youSnake rules.Snake
	for _, snk := range state.Snakes {
		if snake.ID == snk.ID {
			youSnake = snk
			break
		}
	}
	response := ResponsePayload{
		Game: GameResponse{Id: o.GameId, Timeout: o.Timeout},
		Turn: o.Turn,
		Board: BoardResponse{
			Height:  state.Height,
			Width:   state.Width,
			Food:    coordFromPointArray(state.Food),
			Hazards: coordFromPointArray(outOfBounds),
			Snakes:  buildSnakesResponse(o, state.Snakes),
		},
		You: snakeResponseFromSnake(o, youSnake),
	}
	responseJson, err := json.Marshal(response)
	if err != nil {
		log.Panic("[PANIC]: Error Marshalling JSON from State")
		panic(err)
	}
	return responseJson
}

func snakeResponseFromSnake(o *Options, snake rules.Snake) SnakeResponse {
	return SnakeResponse{
		Id:      snake.ID,
		Name:    o.Battlesnakes[snake.ID].Name,
		Health:  snake.Health,
		Body:    coordFromPointArray(snake.Body),
		Latency: "0",
		Head:    coordFromPoint(snake.Body[0]),
		Length:  int32(len(snake.Body)),
		Shout:   "",
		Squad:   o.Battlesnakes[snake.ID].Squad,
	}
}

func buildSnakesResponse(o *Options, snakes []rules.Snake) []SnakeResponse {
	var a []SnakeResponse
	for _, snake := range snakes {
		a = append(a, snakeResponseFromSnake(o, snake))
	}
	return a
}

func coordFromPoint(pt rules.Point) Coord {
	return Coord{X: pt.X, Y: pt.Y}
}

func coordFromPointArray(ptArray []rules.Point) []Coord {
	a := make([]Coord, 0)
	for _, pt := range ptArray {
		a = append(a, coordFromPoint(pt))
	}
	return a
}

func buildSnakesFromOptions(o *Options) []Battlesnake {
	bodyChars := []rune{'■', '⌀', '●', '⍟', '◘', '☺', '□', '☻'}
	var numSnakes int
	var snakes []Battlesnake
	numNames := len(o.Names)
	numURLs := len(o.URLs)
	numSquads := len(o.Squads)
	if numNames > numURLs {
		numSnakes = numNames
	} else {
		numSnakes = numURLs
	}
	if numNames != numURLs {
		log.Println("[WARN]: Number of Names and URLs do not match: defaults will be applied to missing values")
	}
	for i := int(0); i < numSnakes; i++ {
		var snakeName string
		var snakeURL string
		var snakeSquad string

		id := uuid.New().String()

		if i < numNames {
			snakeName = o.Names[i]
		} else {
			log.Printf("[WARN]: Name for URL %v is missing: a default name will be applied\n", o.URLs[i])
			snakeName = id
		}

		if i < numURLs {
			u, err := url.ParseRequestURI(o.URLs[i])
			if err != nil {
				log.Printf("[WARN]: URL %v is not valid: a default will be applied\n", o.URLs[i])
				snakeURL = "https://example.com"
			} else {
				snakeURL = u.String()
			}
		} else {
			log.Printf("[WARN]: URL for Name %v is missing: a default URL will be applied\n", o.Names[i])
			snakeURL = "https://example.com"
		}

		if o.GameType == "squad" {
			if i < numSquads {
				snakeSquad = o.Squads[i]
			} else {
				log.Printf("[WARN]: Squad for URL %v is missing: a default squad will be applied\n", o.URLs[i])
				snakeSquad = strconv.Itoa(i / 2)
			}
		}
		res, err := o.HttpClient.Get(snakeURL)
		api := "0"
		if err != nil {
			log.Printf("[WARN]: Request to %v failed", snakeURL)
		} else if res.Body != nil {
			defer res.Body.Close()
			body, readErr := ioutil.ReadAll(res.Body)
			if readErr != nil {
				log.Fatal(readErr)
			}

			pingResponse := PingResponse{}
			jsonErr := json.Unmarshal(body, &pingResponse)
			if jsonErr != nil {
				log.Fatal(jsonErr)
			} else {
				api = pingResponse.APIVersion
			}
		}
		snake := Battlesnake{Name: snakeName, URL: snakeURL, ID: id, API: api, LastMove: "up", Character: bodyChars[i%8]}
		if o.GameType == "squad" {
			snake.Squad = snakeSquad
		}
		snakes = append(snakes, snake)
	}
	return snakes
}

func printMap(o *Options, state *rules.BoardState, outOfBounds []rules.Point) {
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("Ruleset: %s, Seed: %d, Turn: %v\n", o.GameType, o.Seed, o.Turn))
	board := make([][]rune, state.Width)
	for i := range board {
		board[i] = make([]rune, state.Height)
	}
	for y := int32(0); y < state.Height; y++ {
		for x := int32(0); x < state.Width; x++ {
			board[x][y] = '◦'
		}
	}
	for _, oob := range outOfBounds {
		board[oob.X][oob.Y] = '░'
	}
	b.WriteString(fmt.Sprintf("Hazards ░: %v\n", outOfBounds))
	for _, f := range state.Food {
		board[f.X][f.Y] = '⚕'
	}
	b.WriteString(fmt.Sprintf("Food ⚕: %v\n", state.Food))
	for _, s := range state.Snakes {
		for _, b := range s.Body {
			if b.X < 0 || b.Y < 0 || b.X >= state.Width || b.Y >= state.Height{
				continue
			}
			board[b.X][b.Y] = o.Battlesnakes[s.ID].Character
		}
		b.WriteString(fmt.Sprintf("%v %c: %v\n", o.Battlesnakes[s.ID].Name, o.Battlesnakes[s.ID].Character, s))
	}
	for y := state.Height - 1; y >= 0; y-- {
		for x := int32(0); x < state.Width; x++ {
			b.WriteRune(board[x][y])
		}
		b.WriteString("\n")
	}
	log.Print(b.String())
}
